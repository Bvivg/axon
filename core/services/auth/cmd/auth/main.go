// Command auth runs the authentication service.
//
// This file is wiring only: read configuration, build dependencies, start the
// servers, shut them down cleanly. Anything that decides how authentication
// behaves lives under internal/.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/postgres"

	"github.com/bvivg/axon/core/services/auth/internal/config"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/password"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
	"github.com/bvivg/axon/core/services/auth/internal/server"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

// version is stamped in at build time.
var version = "dev"

func main() {
	// Exits when started with -healthcheck. The runtime image is distroless and
	// has no shell for a container healthcheck to use, so the binary probes
	// itself.
	health.RunProbeIfRequested()

	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails, so this one
		// message goes to stderr directly.
		fmt.Fprintf(os.Stderr, "auth: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(logger.Options{
		Service: cfg.Service,
		Level:   cfg.LogLevel,
		Format:  cfg.LogFormat,
	})

	// Signals cancel this context, which unwinds everything below it.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.InfoContext(ctx, "starting auth service",
		"version", version,
		"environment", string(cfg.Environment),
	)

	pool, err := postgres.Connect(ctx, cfg.Postgres, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	hasher, err := password.NewHasher(cfg.Password)
	if err != nil {
		return err
	}

	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{
		Keys:     cfg.JWT.Keys,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
		TTL:      cfg.JWT.AccessTTL,
	})
	if err != nil {
		return err
	}

	// The service verifies its own tokens against the keys it holds, using the
	// same verifier every other service runs against the JWKS document.
	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     cfg.JWT.Keys,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
	})
	if err != nil {
		return err
	}

	svc, err := service.New(repository.New(pool), hasher, issuer, log, service.Config{
		RefreshTTL: cfg.JWT.RefreshTTL,
	})
	if err != nil {
		return err
	}

	handler, err := server.New(server.Config{Service: svc, Verifier: verifier, Logger: log})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	// Two listeners. The public one serves the contract; the admin one serves
	// probes, metrics and the JWKS document. Keeping them apart is what lets the
	// operational surface stay off whatever the gateway exposes.
	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(handler, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		// gRPC clients need HTTP/2 without TLS inside the compose network, where
		// TLS terminates at the edge rather than here.
		Protocols: unencryptedHTTP2(),
	}

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(cfg, pool, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 2)

	go serve(ctx, publicSrv, "public", log, errs)
	go serve(ctx, adminSrv, "admin", log, errs)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	return shutdown(cfg.ShutdownTimeout, log, publicSrv, adminSrv)
}

// publicHandler builds the contract listener.
func publicHandler(handler *server.Handler, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	interceptors := connectInterceptors(metrics, log)

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(handler, interceptors))

	return middleware.Chain(
		middleware.Correlation,
		middleware.Recovery(log),
		middleware.RequestLogger(log),
	)(mux)
}

// unencryptedHTTP2 enables h2c alongside HTTP/1.1.
//
// gRPC requires HTTP/2, and inside the compose network there is no TLS to
// negotiate it with. HTTP/1.1 stays on because Connect's JSON and gRPC-Web
// transports use it, and that is what the web client and curl speak.
func unencryptedHTTP2() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

// adminHandler builds the internal listener: probes, metrics and JWKS.
func adminHandler(cfg config.Config, pool *postgres.Pool, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	health.New(log, []health.Checker{pool}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("GET /.well-known/jwks.json", jwksHandler(cfg.JWT.Keys, log))

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

// jwksHandler serves the public key set every other service verifies against.
//
// The document is rendered once at startup: the key set does not change while
// the process runs, and re-marshalling it per request would be work for nothing
// on an endpoint that gets polled.
func jwksHandler(keys *jwt.KeySet, log *slog.Logger) http.Handler {
	document, err := keys.JWKS()
	if err != nil {
		// Unreachable in practice — the key set was validated at construction —
		// but serving a broken JWKS silently would break every other service.
		log.Error("could not render the JWKS document", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "jwks unavailable", http.StatusInternalServerError)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwk-set+json")
		// Long enough that verifiers are not polling constantly, short enough
		// that a newly published key propagates without anyone intervening.
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(document)
	})
}

func connectInterceptors(metrics *middleware.Metrics, log *slog.Logger) connect.HandlerOption {
	return connect.WithInterceptors(
		middleware.NewCorrelationInterceptor(),
		middleware.NewRecoveryInterceptor(log),
		metrics.Interceptor(),
		middleware.NewLoggingInterceptor(log),
	)
}

// serve runs one listener, reporting anything but a clean shutdown.
func serve(ctx context.Context, srv *http.Server, name string, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

// shutdown stops both listeners, giving in-flight requests a bounded chance to
// finish. The deadline is not optional: without it a single stuck request keeps
// the process alive until the orchestrator kills it.
func shutdown(timeout time.Duration, log *slog.Logger, servers ...*http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var errs []error
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	log.Info("stopped cleanly")
	return nil
}

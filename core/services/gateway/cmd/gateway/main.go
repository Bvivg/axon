// Command gateway is the public entry point to Axon.
//
// It terminates client traffic, decides who may call what and how often, and
// forwards to the services behind it. Everything past this process is internal
// traffic that has already been through the policy here.
//
// This file is wiring only.
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

	"github.com/bvivg/axon/core/services/gateway/internal/config"
	"github.com/bvivg/axon/core/services/gateway/internal/cors"
	"github.com/bvivg/axon/core/services/gateway/internal/guard"
	"github.com/bvivg/axon/core/services/gateway/internal/proxy"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

// version is stamped in at build time.
var version = "dev"

// sensitiveBurst is how many sign-in attempts may arrive back to back before
// throttling starts.
//
// The limiter's default burst is a tenth of the per-minute rate, which is right
// for browsing traffic but degenerates to one at the rates the sensitive tier
// runs at — and a burst of one means the second mistyped password returns "too
// many requests" instead of "wrong password". Five is a person correcting a
// typo; the sustained rate is what actually shapes a brute-force attempt.
const sensitiveBurst = 5

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.InfoContext(ctx, "starting gateway",
		"version", version,
		"environment", string(cfg.Environment),
		"allowed_origins", cfg.CORS.AllowedOrigins,
	)

	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:             cfg.JWKSURL,
		RefreshInterval: cfg.JWKSRefreshInterval,
		Logger:          log,
	})
	if err != nil {
		return err
	}

	// Fetched once before serving, so a wrong URL or an auth service that never
	// came up fails at startup instead of as a wave of 401s.
	if err := keys.Refresh(ctx); err != nil {
		return fmt.Errorf("initial jwks fetch: %w", err)
	}
	go keys.Start(ctx)

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     keys,
		Issuer:   cfg.JWTIssuer,
		Audience: cfg.JWTAudience,
	})
	if err != nil {
		return err
	}

	standard := ratelimit.New(ratelimit.Config{PerMinute: cfg.RateLimit.StandardPerMinute})
	sensitive := ratelimit.New(ratelimit.Config{
		PerMinute: cfg.RateLimit.SensitivePerMinute,
		Burst:     sensitiveBurst,
	})
	go standard.Start(ctx)
	go sensitive.Start(ctx)

	policyGuard, err := guard.New(guard.Config{
		Verifier:  verifier,
		Standard:  standard,
		Sensitive: sensitive,
		Logger:    log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	upstream := &http.Client{
		Timeout: cfg.UpstreamTimeout,
		// h2c to the internal service: gRPC needs HTTP/2, and there is no TLS
		// inside the compose network to negotiate it with.
		Transport: internalTransport(),
	}

	authClient := proxy.NewClient(upstream, cfg.AuthServiceURL,
		connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
	)

	authProxy, err := proxy.NewAuth(authClient)
	if err != nil {
		return err
	}

	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, authProxy, policyGuard, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         unencryptedHTTP2(),
	}

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(keys, metrics, log),
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

// publicHandler builds the listener clients reach.
func publicHandler(
	cfg config.Config,
	authProxy authv1connect.AuthServiceHandler,
	policyGuard connect.Interceptor,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(authv1connect.NewAuthServiceHandler(authProxy,
		connect.WithInterceptors(
			middleware.NewCorrelationInterceptor(),
			middleware.NewRecoveryInterceptor(log),
			metrics.Interceptor(),
			// The policy runs after correlation and recovery so a refusal is
			// still logged with an id, and before logging so the outcome the
			// log records is the one the client got.
			policyGuard,
			middleware.NewLoggingInterceptor(log),
		),
	))

	return middleware.Chain(
		// The caller's address is captured here, where the connection is still
		// visible; Connect no longer exposes it by the time an interceptor runs.
		guard.ClientIPMiddleware(cfg.RateLimit.TrustedProxies),
		middleware.Correlation,
		middleware.Recovery(log),
		cors.Middleware(cfg.CORS.AllowedOrigins),
		middleware.RequestLogger(log),
	)(mux)
}

// adminHandler builds the internal listener.
func adminHandler(keys *authn.Cache, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	// Readiness depends on having keys: without them every authenticated request
	// would fail, so an instance in that state must not be in rotation.
	health.New(log, []health.Checker{jwksChecker{keys}}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

// jwksChecker reports whether the gateway can verify tokens at all.
type jwksChecker struct{ keys *authn.Cache }

func (c jwksChecker) Name() string { return "jwks" }

func (c jwksChecker) Check(context.Context) error {
	if len(c.keys.KeyIDs()) == 0 {
		return errors.New("no verification keys cached")
	}
	return nil
}

// internalTransport is the transport used for service-to-service calls.
func internalTransport() *http.Transport {
	// Cloned from the default rather than built from zero, so timeouts and pool
	// sizes stay whatever the standard library considers sane. The assertion is
	// checked because DefaultTransport is a package variable anything could have
	// replaced.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	t := base.Clone()

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	t.Protocols = protocols

	return t
}

// unencryptedHTTP2 enables h2c alongside HTTP/1.1: gRPC clients need HTTP/2,
// while Connect's JSON and gRPC-Web transports — what the browser speaks — use
// HTTP/1.1.
func unencryptedHTTP2() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

func serve(ctx context.Context, srv *http.Server, name string, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

// shutdown stops both listeners with a deadline: without one a single stuck
// request keeps the process alive until the orchestrator kills it.
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

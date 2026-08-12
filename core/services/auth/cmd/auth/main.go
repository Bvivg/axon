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
	"github.com/bvivg/axon/core/shared/pkg/redis"

	"github.com/bvivg/axon/core/services/auth/internal/config"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/password"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
	"github.com/bvivg/axon/core/services/auth/internal/server"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

var version = "dev"

func main() {

	health.RunProbeIfRequested()

	if err := run(); err != nil {

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

	cache, err := redis.Connect(ctx, cfg.Redis, log)
	if err != nil {
		return err
	}
	defer func() { _ = cache.Close() }()

	oauthCfg, err := buildOAuth(cfg, cache, log)
	if err != nil {
		return err
	}

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
		OAuth:      oauthCfg,
	})
	if err != nil {
		return err
	}

	handler, err := server.New(server.Config{Service: svc, Verifier: verifier, Logger: log})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, handler, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,

		Protocols: unencryptedHTTP2(),
	}

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(cfg, pool, cache, metrics, log),
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

func buildOAuth(cfg config.Config, cache *redis.Client, log *slog.Logger) (*service.OAuthConfig, error) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  cfg.OAuth.RedirectBaseURL,
		Google:           cfg.OAuth.Google,
		GitHub:           cfg.OAuth.GitHub,
		Apple:            cfg.OAuth.Apple,
		Logger:           log,
		FakeEnabled:      cfg.OAuth.FakeEnabled,
		FakeAuthorizeURL: cfg.OAuth.FakeAuthorizeURL,
		Production:       cfg.Environment.IsProduction(),
	})
	if err != nil {
		return nil, err
	}

	available := registry.Available()
	if len(available) == 0 {
		log.Warn("no oauth provider is configured; provider sign-in is unavailable")
		return nil, nil
	}

	states, err := oauth.NewStateStore(cache, oauth.StateStoreConfig{})
	if err != nil {
		return nil, err
	}

	returnTo, err := oauth.NewReturnToPolicy(cfg.OAuth.AllowedReturnOrigins)
	if err != nil {
		return nil, err
	}

	log.Info("oauth providers ready", "providers", available)

	return &service.OAuthConfig{
		Providers: registry,
		States:    states,
		ReturnTo:  returnTo,
	}, nil
}

func publicHandler(cfg config.Config, handler *server.Handler, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	interceptors := connectInterceptors(metrics, log)

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(handler, interceptors))

	if cfg.OAuth.FakeEnabled {
		mux.Handle("GET "+oauth.FakeAuthorizePath, oauth.FakeAuthorizeHandler())
	}

	return middleware.Chain(
		middleware.Correlation,
		middleware.Recovery(log),
		middleware.RequestLogger(log),
	)(mux)
}

func unencryptedHTTP2() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

func adminHandler(
	cfg config.Config,
	pool *postgres.Pool,
	cache *redis.Client,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	health.New(log, []health.Checker{pool, cache}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("GET /.well-known/jwks.json", jwksHandler(cfg.JWT.Keys, log))

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

func jwksHandler(keys *jwt.KeySet, log *slog.Logger) http.Handler {
	document, err := keys.JWKS()
	if err != nil {

		log.Error("could not render the JWKS document", "error", err)
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "jwks unavailable", http.StatusInternalServerError)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/jwk-set+json")

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

func serve(ctx context.Context, srv *http.Server, name string, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

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

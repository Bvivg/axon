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
	"github.com/prometheus/client_golang/prometheus"

	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/presence"
	"github.com/bvivg/axon/core/shared/pkg/redis"
	"github.com/bvivg/axon/core/shared/pkg/revocation"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/config"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/keyring"
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
	hostname, _ := os.Hostname()
	shutdownTracing, err := telemetry.Setup(ctx, telemetry.FromEnv(cfg.Service, hostname))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()

	identity, err := mtls.LoadEnv()
	if err != nil {
		return err
	}

	log.InfoContext(ctx, "starting auth service",
		"version", version,
		"environment", string(cfg.Environment),
	)

	pool, err := postgres.Connect(ctx, cfg.Postgres, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	go outbox.Sweeper{DB: pool, Table: repository.OutboxTable, Logger: log}.Run(ctx)

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

	ring := jwt.NewRing(cfg.JWT.Keys)
	if cfg.JWT.Rotation.Interval > 0 {
		rotator, err := signingKeys(cfg, pool, ring, log)
		if err != nil {
			return err
		}
		if err := rotator.Sync(ctx); err != nil {
			return fmt.Errorf("signing keys: %w", err)
		}
		go rotator.Run(ctx)
	}

	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{
		Keys:     ring,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
		TTL:      cfg.JWT.AccessTTL,
	})
	if err != nil {
		return err
	}

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     ring,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
	})
	if err != nil {
		return err
	}

	avatarStore, err := avatar.NewStore(avatar.StoreConfig{
		Endpoint:  cfg.Avatar.Endpoint,
		AccessKey: cfg.Avatar.AccessKey,
		SecretKey: cfg.Avatar.SecretKey,
		Bucket:    cfg.Avatar.Bucket,
		UseSSL:    cfg.Avatar.UseSSL,
	})
	if err != nil {
		return err
	}
	avatarURLs := avatar.NewURLBuilder(cfg.Avatar.PublicURL, cfg.Avatar.Bucket)
	avatarPipeline := avatar.NewPipeline(avatarStore, avatarURLs, nil)

	revocationPublisher, err := revocation.NewPublisher(cache)
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)
	auditFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "auth_audit_write_failures_total",
		Help: "Security events that could not be written to the auth outbox.",
	})
	metrics.Registry().MustRegister(auditFailures)

	svc, err := service.New(repository.New(pool), hasher, issuer, log, service.Config{
		RefreshTTL:    cfg.JWT.RefreshTTL,
		OAuth:         oauthCfg,
		Avatar:        avatarPipeline,
		Presence:      presence.NewTracker(cache),
		Revocation:    revocationPublisher,
		AuditFailures: auditFailures,
	})
	if err != nil {
		return err
	}

	handler, err := server.New(server.Config{
		Service:    svc,
		Verifier:   verifier,
		Logger:     log,
		AvatarURLs: avatarURLs,
	})
	if err != nil {
		return err
	}

	checks := health.New(log, []health.Checker{pool, cache})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.ShutdownTimeout, checks, log)
	publicSrv := graceful.Serve(&http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, handler, ring, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,

		Protocols: mtls.ServerProtocols(identity),
	})

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(ring, checks, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 2)

	go serve(ctx, publicSrv, "public", identity, log, errs)
	go serve(ctx, adminSrv, "admin", nil, log, errs)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	return graceful.Stop(publicSrv, adminSrv)
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

func publicHandler(cfg config.Config, handler *server.Handler, keys jwt.Keys, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	interceptors := connectInterceptors(metrics, log)

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(handler, interceptors))
	if len(cfg.InternalKey.Reveal()) >= 32 {
		mux.Handle(authv1connect.NewSessionControlServiceHandler(&server.SessionControl{Handler: handler, Key: cfg.InternalKey.Reveal()}))
	}
	mux.HandleFunc("POST /internal/avatar", handler.UploadAvatar)
	mux.Handle("GET /.well-known/jwks.json", jwksHandler(keys, log))

	if cfg.OAuth.FakeEnabled {
		mux.Handle("GET "+oauth.FakeAuthorizePath, oauth.FakeAuthorizeHandler())
	}

	return middleware.Chain(
		middleware.Correlation,
		middleware.Recovery(log),
		middleware.RequestLogger(log),
	)(mux)
}

func adminHandler(
	keys jwt.Keys,
	checks *health.Handler,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	checks.Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	telemetry.Profiling(mux)
	mux.Handle("GET /.well-known/jwks.json", jwksHandler(keys, log))

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

func jwksHandler(keys jwt.Keys, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		document, err := keys.JWKS()
		if err != nil {
			log.ErrorContext(r.Context(), "could not render the JWKS document", "error", err)
			http.Error(w, "jwks unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		w.Header().Set("Cache-Control", "public, max-age=60")
		_, _ = w.Write(document)
	})
}

func signingKeys(cfg config.Config, pool *postgres.Pool, ring *jwt.Ring, log *slog.Logger) (*keyring.Rotator, error) {
	if len(cfg.JWT.Rotation.EncryptionKey) == 0 {
		return nil, errors.New("JWT_KEY_ENCRYPTION_KEY is required when JWT_ROTATION_INTERVAL is set")
	}
	sealer, err := keyring.NewSealer(cfg.JWT.Rotation.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return keyring.New(keyring.Config{
		Store:  keyring.NewPostgres(pool.Pool),
		Sealer: sealer,
		Ring:   ring,
		Policy: keyring.Policy{
			Interval: cfg.JWT.Rotation.Interval,
			Lead:     cfg.JWT.Rotation.Lead,
			Retain:   cfg.JWT.AccessTTL + time.Minute,
		},
		Bootstrap: cfg.JWT.Keys.Active(),
		Extra:     cfg.JWT.Retired,
		Check:     cfg.JWT.Rotation.Check,
		Log:       log.With("component", "keyring"),
	})
}

func connectInterceptors(metrics *middleware.Metrics, log *slog.Logger) connect.HandlerOption {
	return connect.WithInterceptors(
		telemetry.Interceptor(),
		middleware.NewCorrelationInterceptor(),
		middleware.NewRecoveryInterceptor(log),
		metrics.Interceptor(),
		middleware.NewLoggingInterceptor(log),
		server.DeviceInterceptor(),
	)
}

func serve(ctx context.Context, srv *http.Server, name string, identity *mtls.Identity, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr, "mtls", identity != nil)

	if err := mtls.ListenAndServe(srv, identity); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

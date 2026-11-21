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
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/services/tournament/internal/consumer"
	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/server"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/rpclimit"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

func main() {
	health.RunProbeIfRequested()
	if err := run(); err != nil {
		slog.Error("tournament service stopped", "error", err)
		os.Exit(1)
	}
}

type settings struct {
	base          config.Base
	postgres      postgres.Config
	brokers       []string
	finishedTopic string
	ratingTopic   string
	group         string
	noShow        time.Duration
	sweep         time.Duration
	retention     time.Duration
	games         []string
	authURL       string
	jwksURL       string
	jwksRefresh   time.Duration
	issuer        string
	audience      string
}

func load() (settings, error) {
	loader := config.NewLoader()
	cfg := settings{
		base:          config.LoadBase(loader, "tournament"),
		postgres:      postgres.LoadConfig(loader),
		brokers:       loader.StringSlice("KAFKA_BROKERS", []string{"kafka:29092"}),
		finishedTopic: loader.StringDefault("TOURNAMENT_FINISHED_TOPIC", consumer.FinishedTopic),
		ratingTopic:   loader.StringDefault("TOURNAMENT_RATING_TOPIC", consumer.RatingTopic),
		group:         loader.StringDefault("TOURNAMENT_CONSUMER_GROUP", consumer.Group),
		noShow:        loader.DurationDefault("TOURNAMENT_NO_SHOW_TIMEOUT", orchestrator.DefaultNoShow),
		sweep:         loader.DurationDefault("TOURNAMENT_SWEEP_INTERVAL", time.Second),
		retention:     loader.DurationDefault("TOURNAMENT_OUTBOX_RETENTION", 24*time.Hour),
		games:         loader.StringSlice("TOURNAMENT_GAMES", []string{"tictactoe", "chess", "quoridor", "checkers"}),
		authURL:       loader.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
		jwksURL:       loader.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		jwksRefresh:   loader.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		issuer:        loader.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
		audience:      loader.StringDefault("JWT_AUDIENCE", "axon"),
	}
	if cfg.noShow < time.Second {
		loader.Fail("TOURNAMENT_NO_SHOW_TIMEOUT", errors.New("must be at least a second"))
	}
	if cfg.sweep <= 0 || cfg.sweep > cfg.noShow {
		loader.Fail("TOURNAMENT_SWEEP_INTERVAL", errors.New("must be positive and not longer than the no-show timeout"))
	}
	if len(cfg.games) == 0 {
		loader.Fail("TOURNAMENT_GAMES", errors.New("must list at least one game"))
	}
	return cfg, loader.Err()
}

func run() error {
	cfg, err := load()
	if err != nil {
		return err
	}
	log := cfg.base.Logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hostname, _ := os.Hostname()
	shutdownTracing, err := telemetry.Setup(ctx, telemetry.FromEnv(cfg.base.Service, hostname))
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
	pool, err := postgres.Connect(ctx, cfg.postgres, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	keys, err := authn.NewCache(authn.JWKSConfig{
		URL: cfg.jwksURL, HTTPClient: mtls.HTTPClient(identity, 10*time.Second), RefreshInterval: cfg.jwksRefresh, Logger: log,
	})
	if err != nil {
		return err
	}
	if err := keys.WaitUntilReady(ctx, authn.DefaultStartupTimeout); err != nil {
		return fmt.Errorf("initial jwks fetch: %w", err)
	}
	go keys.Start(ctx)
	verifier, err := authn.NewVerifier(authn.Config{Keys: keys, Issuer: cfg.issuer, Audience: cfg.audience})
	if err != nil {
		return err
	}
	probe, err := kgo.NewClient(kgo.SeedBrokers(cfg.brokers...))
	if err != nil {
		return err
	}
	defer probe.Close()

	metrics := middleware.NewMetrics(cfg.base.Service)
	tournaments := store.New(pool.Pool)
	saga := orchestrator.New(orchestrator.Config{Store: tournaments, NoShow: cfg.noShow, Metrics: orchestrator.NewMetrics(metrics.Registry()), Log: log})
	events := consumer.New(consumer.Config{
		Brokers: cfg.brokers, FinishedTopic: cfg.finishedTopic, RatingTopic: cfg.ratingTopic, Group: cfg.group, Recorder: saga, Ratings: tournaments, Log: log,
	})
	go func() { _ = events.Run(ctx) }()
	go saga.Run(ctx, cfg.sweep)
	go tournaments.PruneOutbox(ctx, time.Hour, cfg.retention, log)
	log.InfoContext(ctx, "tournament saga running", "no_show_timeout", cfg.noShow, "sweep_interval", cfg.sweep, "games", cfg.games)

	interceptors := connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log),
		metrics.Interceptor(), middleware.NewLoggingInterceptor(log))
	roles := server.NewAuthRoles(&http.Client{Timeout: 10 * time.Second, Transport: telemetry.Transport(mtls.Internal(identity))}, cfg.authURL,
		connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor()))
	checks := health.New(log, []health.Checker{pool, health.CheckerFunc{CheckerName: "kafka", Fn: probe.Ping}})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.base.ShutdownTimeout, checks, log)
	api := http.NewServeMux()
	api.Handle(tournamentv1connect.NewTournamentServiceHandler(server.New(saga, verifier, roles, cfg.games, log), interceptors, rpclimit.Handler(cfg.base.RPCReadMaxBytes)))
	public := graceful.Serve(&http.Server{
		Addr:              cfg.base.HTTPAddr,
		Handler:           middleware.Chain(middleware.Correlation, middleware.Recovery(log))(api),
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         mtls.ServerProtocols(identity),
	})
	admin := http.NewServeMux()
	checks.Register(admin)
	admin.Handle("/metrics", metrics.Handler())
	telemetry.Profiling(admin)
	adminSrv := &http.Server{
		Addr:              cfg.base.MetricsAddr,
		Handler:           middleware.Chain(middleware.Correlation, middleware.Recovery(log))(admin),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errs := make(chan error, 2)
	go listen(ctx, public, "public", identity, log, errs)
	go listen(ctx, adminSrv, "admin", nil, log, errs)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}
	return graceful.Stop(public, adminSrv)
}

func listen(ctx context.Context, srv *http.Server, name string, identity *mtls.Identity, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr, "mtls", identity != nil)
	if err := mtls.ListenAndServe(srv, identity); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

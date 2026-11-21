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
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/services/analysis/internal/analyser"
	"github.com/bvivg/axon/core/services/analysis/internal/consumer"
	"github.com/bvivg/axon/core/services/analysis/internal/server"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	"github.com/bvivg/axon/core/services/analysis/internal/uci"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
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
		slog.Error("analysis service stopped", "error", err)
		os.Exit(1)
	}
}

type settings struct {
	base        config.Base
	postgres    postgres.Config
	brokers     []string
	topic       string
	group       string
	stockfish   string
	engines     int
	depth       int
	timeout     time.Duration
	hashMB      int
	maxPlies    int
	jwksURL     string
	jwksRefresh time.Duration
	issuer      string
	audience    string
}

func within(loader *config.Loader, key string, value, low, high int) {
	if value < low || value > high {
		loader.Fail(key, fmt.Errorf("must be between %d and %d, got %d", low, high, value))
	}
}

func load() (settings, error) {
	loader := config.NewLoader()
	cfg := settings{
		base:        config.LoadBase(loader, "analysis"),
		postgres:    postgres.LoadConfig(loader),
		brokers:     loader.StringSlice("KAFKA_BROKERS", []string{"kafka:29092"}),
		topic:       loader.StringDefault("ANALYSIS_TOPIC", consumer.Topic),
		group:       loader.StringDefault("ANALYSIS_CONSUMER_GROUP", consumer.Group),
		stockfish:   loader.StringDefault("STOCKFISH_PATH", "stockfish"),
		engines:     loader.IntDefault("ANALYSIS_ENGINES", 2),
		depth:       loader.IntDefault("ANALYSIS_DEPTH", 12),
		timeout:     loader.DurationDefault("ANALYSIS_POSITION_TIMEOUT", 10*time.Second),
		hashMB:      loader.IntDefault("ANALYSIS_ENGINE_HASH_MB", 16),
		maxPlies:    loader.IntDefault("ANALYSIS_MAX_PLIES", consumer.DefaultMaxPlies),
		jwksURL:     loader.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		jwksRefresh: loader.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		issuer:      loader.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
		audience:    loader.StringDefault("JWT_AUDIENCE", "axon"),
	}
	within(loader, "ANALYSIS_ENGINES", cfg.engines, 1, 32)
	within(loader, "ANALYSIS_DEPTH", cfg.depth, 1, 40)
	within(loader, "ANALYSIS_ENGINE_HASH_MB", cfg.hashMB, 1, 1024)
	within(loader, "ANALYSIS_MAX_PLIES", cfg.maxPlies, 1, 2000)
	if cfg.timeout <= 0 {
		loader.Fail("ANALYSIS_POSITION_TIMEOUT", errors.New("must be positive"))
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

	metrics := middleware.NewMetrics(cfg.base.Service)
	restarts := prometheus.NewCounter(prometheus.CounterOpts{Name: "analysis_engine_restarts_total", Help: "Stockfish processes replaced after they exited or stopped answering."})
	metrics.Registry().MustRegister(restarts)
	engineCtx, stopEngines := context.WithCancel(ctx)
	engines, err := uci.NewPool(engineCtx, uci.PoolConfig{
		Size: cfg.engines, Depth: cfg.depth, Timeout: cfg.timeout, OnRestart: restarts.Inc, Log: log.With("component", "engines"),
		Start: func(ctx context.Context) (*uci.Engine, error) {
			return uci.Start(ctx, cfg.stockfish, uci.Options{Threads: 1, HashMB: cfg.hashMB})
		},
	})
	if err != nil {
		stopEngines()
		return err
	}
	defer func() {
		stopEngines()
		engines.Close()
	}()
	log.InfoContext(ctx, "engines ready", "engine", engines.Name(), "engines", cfg.engines, "depth", cfg.depth, "position_timeout", cfg.timeout)

	probe, err := kgo.NewClient(kgo.SeedBrokers(cfg.brokers...))
	if err != nil {
		return err
	}
	defer probe.Close()
	reports := store.New(pool.Pool)
	games := consumer.New(consumer.Config{
		Brokers: cfg.brokers, Topic: cfg.topic, Group: cfg.group, Store: reports, Analyser: analyser.New(engines),
		Depth: cfg.depth, Engine: engines.Name(), MaxPlies: cfg.maxPlies, Log: log, Metrics: consumer.NewMetrics(metrics.Registry()),
	})
	go func() { _ = games.Run(ctx) }()

	checks := health.New(log, []health.Checker{
		pool,
		health.CheckerFunc{CheckerName: "kafka", Fn: probe.Ping},
		health.CheckerFunc{CheckerName: "stockfish", Fn: engines.Check},
	})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.base.ShutdownTimeout, checks, log)
	api := http.NewServeMux()
	api.Handle(analysisv1connect.NewAnalysisServiceHandler(server.New(reports, verifier, log), rpclimit.Handler(cfg.base.RPCReadMaxBytes), connect.WithInterceptors(
		telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
	)))
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

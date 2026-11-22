package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/bvivg/axon/core/services/rating/internal/consumer"
	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/rebuild"
	"github.com/bvivg/axon/core/services/rating/internal/server"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/redis"
	"github.com/bvivg/axon/core/shared/pkg/rpclimit"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

var (
	rebuildMode  = flag.Bool("rebuild", false, "recompute every rating from game.finished.v1 and compare with rating.ratings and the leaderboards; exits 2 on a difference")
	applyRebuild = flag.Bool("apply", false, "with -rebuild, replace rating.ratings and the leaderboards with the recomputed values")
)

const exitDifferent = 2

func main() {
	health.RunProbeIfRequested()
	code, err := run()
	if err != nil {
		slog.Error("rating service stopped", "error", err)
		os.Exit(1)
	}
	os.Exit(code)
}

type settings struct {
	base      config.Base
	postgres  postgres.Config
	redis     redis.Config
	brokers   []string
	topic     string
	group     string
	retention time.Duration
	authURL   string
	jwksURL   string
	jwksEvery time.Duration
	issuer    string
	audience  string
}

func load() (settings, error) {
	loader := config.NewLoader()
	cfg := settings{
		base:      config.LoadBase(loader, "rating"),
		postgres:  postgres.LoadConfig(loader),
		redis:     redis.LoadConfig(loader),
		brokers:   loader.StringSlice("KAFKA_BROKERS", []string{"kafka:29092"}),
		topic:     loader.StringDefault("RATING_FINISHED_TOPIC", ledger.FinishedTopic),
		group:     loader.StringDefault("RATING_CONSUMER_GROUP", consumer.Group),
		retention: loader.DurationDefault("RATING_OUTBOX_RETENTION", 24*time.Hour),
		authURL:   loader.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
		jwksURL:   loader.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		jwksEvery: loader.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		issuer:    loader.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
		audience:  loader.StringDefault("JWT_AUDIENCE", "axon"),
	}
	return cfg, loader.Err()
}

func run() (int, error) {
	if *applyRebuild && !*rebuildMode {
		return 0, errors.New("-apply only makes sense with -rebuild")
	}
	cfg, err := load()
	if err != nil {
		return 0, err
	}
	log := cfg.base.Logger()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	hostname, _ := os.Hostname()
	shutdownTracing, err := telemetry.Setup(ctx, telemetry.FromEnv(cfg.base.Service, hostname))
	if err != nil {
		return 0, err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()
	pool, err := postgres.Connect(ctx, cfg.postgres, log)
	if err != nil {
		return 0, err
	}
	defer pool.Close()
	cache, err := redis.Connect(ctx, cfg.redis, log)
	if err != nil {
		return 0, err
	}
	defer func() { _ = cache.Close() }()
	ratings := store.New(pool.Pool, glicko.Default)
	board := leaderboard.New(cache.Client)
	if *rebuildMode {
		return rebuildRatings(ctx, cfg, ratings, board, log)
	}
	return 0, serve(ctx, cfg, pool, cache, ratings, board, log)
}

func rebuildRatings(ctx context.Context, cfg settings, ratings *store.Postgres, board *leaderboard.Redis, log *slog.Logger) (int, error) {
	started := time.Now()
	book, stats, err := rebuild.Recompute(ctx, rebuild.Kafka{Brokers: cfg.brokers, Topic: cfg.topic}, glicko.Default)
	if err != nil {
		return 0, err
	}
	log.InfoContext(ctx, "ratings recomputed", "topic", cfg.topic, "events", stats.Events, "applied", stats.Applied,
		"duplicates", stats.Duplicates, "skipped", stats.Skipped, "invalid", stats.Invalid, "players", len(book.Players),
		"duration_ms", time.Since(started).Milliseconds())
	if *applyRebuild {
		if err := rebuild.Apply(ctx, book, ratings, board); err != nil {
			return 0, err
		}
		log.InfoContext(ctx, "rebuilt ratings applied", "players", len(book.Players), "changes", len(book.Changes))
		return 0, nil
	}
	differences, err := rebuild.Compare(ctx, book, ratings, board)
	if err != nil {
		return 0, err
	}
	for _, difference := range differences {
		log.WarnContext(ctx, "rating differs", "store", difference.Store, "game", difference.Game, "user_id", difference.User,
			"live", difference.Live, "rebuilt", difference.Rebuilt)
	}
	if len(differences) > 0 {
		log.ErrorContext(ctx, "rebuild differs from the live ratings", "differences", len(differences))
		return exitDifferent, nil
	}
	log.InfoContext(ctx, "rebuild matches the live ratings", "players", len(book.Players), "games", len(book.Games()))
	return 0, nil
}

func serve(ctx context.Context, cfg settings, pool *postgres.Pool, cache *redis.Client, ratings *store.Postgres, board *leaderboard.Redis, log *slog.Logger) error {
	probe, err := kgo.NewClient(kgo.SeedBrokers(cfg.brokers...))
	if err != nil {
		return err
	}
	defer probe.Close()
	if err := rebuild.Sync(ctx, ratings, board); err != nil {
		log.WarnContext(ctx, "initial leaderboard sync failed", "error", err)
	}
	metrics := middleware.NewMetrics(cfg.base.Service)
	events := consumer.New(consumer.Config{
		Brokers: cfg.brokers, Topic: cfg.topic, Group: cfg.group, Store: ratings, Board: board, Log: log,
		Events: consumer.NewEventCounter(metrics.Registry()),
	})
	go func() { _ = events.Run(ctx) }()
	go resync(ctx, events, ratings, board, log)
	go ratings.PruneOutbox(ctx, time.Hour, cfg.retention, log)

	kafkaCheck := health.CheckerFunc{CheckerName: "kafka", Fn: probe.Ping}
	checks := health.New(log, []health.Checker{pool, cache, kafkaCheck})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.base.ShutdownTimeout, checks, log)

	identity, err := mtls.LoadEnv()
	if err != nil {
		return err
	}
	keys, err := authn.NewCache(authn.JWKSConfig{
		URL: cfg.jwksURL, HTTPClient: mtls.HTTPClient(identity, 10*time.Second), RefreshInterval: cfg.jwksEvery, Logger: log,
	})
	if err != nil {
		return err
	}
	go keys.Start(ctx)
	verifier, err := authn.NewVerifier(authn.Config{Keys: keys, Issuer: cfg.issuer, Audience: cfg.audience})
	if err != nil {
		return err
	}
	permissions := authz.NewCache(authz.NewRemote(&http.Client{Timeout: 5 * time.Second, Transport: telemetry.Transport(mtls.Internal(identity))}, cfg.authURL), authz.DefaultTTL)

	api := http.NewServeMux()
	api.Handle(ratingv1connect.NewRatingServiceHandler(server.New(board, ratings, log), rpclimit.Handler(cfg.base.RPCReadMaxBytes), connect.WithInterceptors(
		telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
	)))
	api.Handle(ratingv1connect.NewRatingAdminServiceHandler(server.NewAdmin(ratings, log), rpclimit.Handler(cfg.base.RPCReadMaxBytes), connect.WithInterceptors(
		telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
		authz.NewInterceptor(authz.Config{Service: "rating", Verifier: verifier, Cache: permissions, Rules: server.AdminRules, Record: authz.OutboxRecorder(pool, ledger.OutboxTable), Logger: log}),
		admincrud.Validator(),
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

func resync(ctx context.Context, events *consumer.Consumer, ratings *store.Postgres, board *leaderboard.Redis, log *slog.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	pending := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if pending = events.Stale() || pending; !pending {
			continue
		}
		if err := rebuild.Sync(ctx, ratings, board); err != nil {
			log.WarnContext(ctx, "leaderboard resync failed", "error", err)
			continue
		}
		pending = false
		log.InfoContext(ctx, "leaderboard resynced from postgres")
	}
}

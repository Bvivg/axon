package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/game/internal/cluster"
	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/route"
	"github.com/bvivg/axon/core/services/game/internal/server"
	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

func main() {
	health.RunProbeIfRequested()
	if err := run(); err != nil {
		slog.Error("game service stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	loader := config.NewLoader()
	cfg := config.LoadBase(loader, "game")
	db := postgres.LoadConfig(loader)
	log := cfg.Logger()
	keyConfig := authn.JWKSConfig{
		URL:             loader.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		RefreshInterval: loader.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		Logger:          log,
	}
	verifierConfig := authn.Config{
		Issuer:   loader.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
		Audience: loader.StringDefault("JWT_AUDIENCE", "axon"),
	}
	hostname, _ := os.Hostname()
	_, port, _ := strings.Cut(cfg.HTTPAddr, ":")
	instance := loader.StringDefault("GAME_INSTANCE_ID", hostname)
	advertise := loader.StringDefault("GAME_ADVERTISE_ADDR", hostname+":"+port)
	kafka := journal.KafkaConfig{
		Brokers:           loader.StringSlice("KAFKA_BROKERS", []string{"kafka:9092"}),
		Topic:             loader.StringDefault("GAME_JOURNAL_TOPIC", "game.session-events.v1"),
		LeaseTopic:        loader.StringDefault("GAME_LEASE_TOPIC", "game.partition-leases.v1"),
		Partitions:        int32(loader.IntDefault("GAME_JOURNAL_PARTITIONS", 24)),
		ReplicationFactor: int16(loader.IntDefault("GAME_JOURNAL_REPLICATION", 1)),
		MinInSync:         loader.IntDefault("GAME_JOURNAL_MIN_INSYNC", 1),
		Linger:            loader.DurationDefault("GAME_JOURNAL_LINGER", time.Millisecond),
	}
	leaseTTL := loader.DurationDefault("GAME_LEASE_TTL", 6*time.Second)
	testHooks := loader.Bool("GAME_TEST_HOOKS", false)
	if err := loader.Err(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	pool, err := postgres.Connect(ctx, db, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	keys, err := authn.NewCache(keyConfig)
	if err != nil {
		return err
	}
	if err := keys.WaitUntilReady(ctx, authn.DefaultStartupTimeout); err != nil {
		return err
	}
	go keys.Start(ctx)
	verifierConfig.Keys = keys
	verifier, err := authn.NewVerifier(verifierConfig)
	if err != nil {
		return err
	}
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition(), quoridor.Definition()} {
		if err := games.Register(def); err != nil {
			return err
		}
	}
	store := session.NewStore(pool.Pool, games)
	rules := session.NewRules(games)
	model := projection.New(store, rules)
	log = log.With("instance", instance)
	journalLog, err := journal.NewKafka(ctx, kafka)
	if err != nil {
		return err
	}
	defer journalLog.Close()
	own := owner.New(owner.Config{ID: instance, Address: advertise, Log: journalLog, Model: model, Rules: rules, Now: time.Now, Lease: leaseTTL})
	routes := route.New(model.Owners)
	handler := server.New(server.Config{Store: store, Owner: own, Routes: routes, Self: advertise, Verifier: verifier, TestHooks: testHooks})
	own.Listen(handler)
	go own.KeepLeases(ctx)
	go routes.Run(ctx, pool.Pool, projection.OwnerChannel, log)
	go handler.Watch(ctx, log)
	go handler.RunMatchmaking(ctx, log)
	go func() { _ = cluster.NewProjector(kafka, model, log).Run(ctx) }()
	ownership := make(chan struct{})
	go func() {
		defer close(ownership)
		_ = cluster.NewOwnership(kafka, own, log).Run(ctx)
	}()
	metrics := middleware.NewMetrics(cfg.Service)
	mux := http.NewServeMux()
	mux.Handle(gamev1connect.NewGameServiceHandler(handler, connect.WithInterceptors(
		middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
	)))
	mux.Handle(gamev1connect.NewGameOwnerServiceHandler(handler, connect.WithInterceptors(
		middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(),
	)))
	mux.HandleFunc("GET /ws", handler.Socket)
	if testHooks {
		mux.HandleFunc("POST "+server.ClockHookPath, handler.AdjustClock)
	}
	health.New(log, []health.Checker{pool, journalLog}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	srv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		<-ownership
		return srv.Shutdown(shutdown)
	}
}

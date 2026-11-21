package main

import (
	"context"
	"errors"
	"log/slog"
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
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	gamemetrics "github.com/bvivg/axon/core/services/game/internal/metrics"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/rating"
	"github.com/bvivg/axon/core/services/game/internal/route"
	"github.com/bvivg/axon/core/services/game/internal/server"
	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/services/game/internal/tournament"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

const movePartitionsAhead = 3

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
	dbs := loadDatabases(loader)
	log := cfg.Logger()
	identity, err := mtls.LoadEnv()
	if err != nil {
		return err
	}
	adminAddr := loader.StringDefault("GAME_ADMIN_ADDR", "")
	keyConfig := authn.JWKSConfig{
		HTTPClient:      mtls.HTTPClient(identity, 10*time.Second),
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
	ratingTopic := loader.StringDefault("GAME_RATING_TOPIC", rating.Topic)
	tournamentTopics := loader.StringSlice("GAME_TOURNAMENT_TOPICS", []string{tournament.RoundStartedTopic, tournament.ForfeitTopic, tournament.CancelledTopic})
	outboxRetention := loader.DurationDefault("GAME_OUTBOX_RETENTION", 24*time.Hour)
	leaseTTL := loader.DurationDefault("GAME_LEASE_TTL", 6*time.Second)
	finishedTTL := loader.DurationDefault("GAME_FINISHED_TTL", 2*time.Minute)
	appendTimeout := loader.DurationDefault("GAME_APPEND_TIMEOUT", 15*time.Second)
	drain := loader.DurationDefault("SHUTDOWN_DRAIN", 0)
	testHooks, ownerKey := loadControls(loader, cfg.Environment)
	spectatorLimit := loader.IntDefault("GAME_SPECTATOR_LIMIT", 200)
	chessGame, quoridorGame, durakGame := chess.Definition(), quoridor.Definition(), durak.Definition()
	chessGame.MaxPlies = loader.IntDefault("GAME_CHESS_MAX_PLIES", chess.MaxPlies)
	quoridorGame.MaxPlies = loader.IntDefault("GAME_QUORIDOR_MAX_PLIES", quoridor.MaxPlies)
	durakGame.MaxPlies = loader.IntDefault("GAME_DURAK_MAX_PLIES", durak.MaxPlies)
	history := server.HistoryLimits{
		Plies:   loader.IntDefault("GAME_HISTORY_MAX_PLIES", server.DefaultHistoryPlies),
		Bytes:   loader.IntDefault("GAME_HISTORY_MAX_BYTES", server.DefaultHistoryBytes),
		Entries: loader.IntDefault("GAME_HISTORY_CACHE_ENTRIES", server.DefaultHistoryEntries),
	}
	matchSockets := loader.IntDefault("GAME_MATCH_SOCKETS_PER_USER", server.DefaultMatchSockets)
	if err := loader.Err(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownTracing, err := telemetry.Setup(ctx, telemetry.FromEnv(cfg.Service, instance))
	if err != nil {
		return err
	}
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(flush)
	}()
	db, err := dbs.connect(ctx, log)
	if err != nil {
		return err
	}
	defer db.Close()
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
	for _, def := range []engine.Definition{tictactoe.Definition(), chessGame, quoridorGame, durakGame} {
		if err := games.Register(def); err != nil {
			return err
		}
	}
	store := session.NewStore(db.primary.Pool, games, db.storeOptions()...)
	metrics := middleware.NewMetrics(cfg.Service)
	observed := gamemetrics.New(metrics.Registry())
	rules := session.NewRules(games)
	model := projection.New(store, rules, projection.LeasesOn(db.leases.Pool))
	log = log.With("instance", instance)
	journalLog, err := journal.NewKafka(ctx, kafka)
	if err != nil {
		return err
	}
	defer journalLog.Close()
	own := owner.New(owner.Config{ID: instance, Address: advertise, Log: journalLog, Model: model, Rules: rules, Now: time.Now, Lease: leaseTTL, Metrics: observed,
		AppendTimeout: appendTimeout, FinishedTTL: finishedTTL})
	routes := route.New(model.Owners)
	peerTransport := http.DefaultTransport
	var sockets *http.Client
	if identity != nil {
		peerTransport = identity.Transport(mtls.ServerName("game"))
		sockets = &http.Client{Transport: identity.Transport(mtls.ServerName("game"), mtls.HTTP1Only())}
	}
	handler := server.New(server.Config{Store: store, Owner: own, Routes: routes, Self: advertise, Verifier: verifier, TestHooks: testHooks, Metrics: observed, Watchers: spectatorLimit, OwnerKey: ownerKey,
		Client: &http.Client{Timeout: 15 * time.Second, Transport: telemetry.Transport(peerTransport)}, Sockets: sockets, Secure: identity != nil, History: history, MatchSockets: matchSockets})
	own.Listen(handler)
	go own.KeepLeases(ctx)
	go own.KeepEvicting(ctx)
	go routes.Run(ctx, db.listen, projection.OwnerChannel, log)
	go model.MaintainMovePartitions(ctx, movePartitionsAhead, 24*time.Hour, log)
	go handler.Watch(ctx, log)
	go handler.RunMatchmaking(ctx, log)
	go func() { _ = cluster.NewProjector(kafka, model, log, observed).Run(ctx) }()
	go func() { _ = rating.NewConsumer(kafka.Brokers, ratingTopic, store, log).Run(ctx) }()
	go func() {
		_ = tournament.NewConsumer(tournament.Config{
			Brokers: kafka.Brokers, Topics: tournamentTopics, Games: handler, Cancellations: tournament.NewPostgres(db.primary.Pool),
			Log: log, Commands: tournament.NewCounter(metrics.Registry()),
		}).Run(ctx)
	}()
	go model.PruneOutbox(ctx, time.Hour, outboxRetention, log)
	go reportActive(ctx, own, observed)
	ownership := make(chan struct{})
	go func() {
		defer close(ownership)
		_ = cluster.NewOwnership(kafka, own, log).Run(ctx)
	}()
	mux := http.NewServeMux()
	mux.Handle(gamev1connect.NewGameServiceHandler(handler, connect.WithInterceptors(
		telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
	)))
	mux.Handle(gamev1connect.NewGameOwnerServiceHandler(handler, connect.WithInterceptors(
		telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(),
	)))
	mux.HandleFunc("GET /ws", handler.Socket)
	if testHooks {
		mux.HandleFunc("POST "+server.ClockHookPath, handler.AdjustClock)
	}
	checks := health.New(log, []health.Checker{db.primary, journalLog})
	admin := mux
	if adminAddr != "" {
		admin = http.NewServeMux()
	}
	checks.Register(admin)
	admin.Handle("/metrics", metrics.Handler())
	telemetry.Profiling(admin)
	graceful := lifecycle.New(drain, cfg.ShutdownTimeout, checks, log)
	srv := graceful.Serve(&http.Server{
		Addr: cfg.HTTPAddr, Handler: middleware.Chain(middleware.Correlation, middleware.Recovery(log), handler.GuardOwner)(mux),
		ReadHeaderTimeout: 5 * time.Second,
	})
	if identity != nil {
		srv.Protocols = mtls.ServerProtocols(identity)
	}
	servers := []*http.Server{srv}
	done := make(chan error, 2)
	go func() { done <- mtls.ListenAndServe(srv, identity) }()
	if adminAddr != "" {
		adminSrv := &http.Server{Addr: adminAddr, Handler: admin, ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, adminSrv)
		go func() { done <- adminSrv.ListenAndServe() }()
	}
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		<-ownership
		return graceful.Stop(servers...)
	}
}

func reportActive(ctx context.Context, own *owner.Owner, observed *gamemetrics.Game) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			observed.Active(own.Active())
		}
	}
}

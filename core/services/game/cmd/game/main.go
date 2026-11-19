package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
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
	if err := games.Register(tictactoe.Definition()); err != nil {
		return err
	}
	handler := server.New(session.NewStore(pool.Pool, games), verifier)
	go handler.Watch(ctx, log)
	go handler.RunClocks(ctx, log)
	go handler.RunMatchmaking(ctx, log)
	metrics := middleware.NewMetrics(cfg.Service)
	mux := http.NewServeMux()
	mux.Handle(gamev1connect.NewGameServiceHandler(handler, connect.WithInterceptors(
		middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log),
	)))
	mux.HandleFunc("GET /ws", handler.Socket)
	health.New(log, []health.Checker{pool}).Register(mux)
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
		return srv.Shutdown(shutdown)
	}
}

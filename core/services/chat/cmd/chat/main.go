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

	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/presence"
	"github.com/bvivg/axon/core/shared/pkg/redis"

	"github.com/bvivg/axon/core/services/chat/internal/config"
	"github.com/bvivg/axon/core/services/chat/internal/events"
	"github.com/bvivg/axon/core/services/chat/internal/identity"
	chatpresence "github.com/bvivg/axon/core/services/chat/internal/presence"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
	"github.com/bvivg/axon/core/services/chat/internal/server"
	"github.com/bvivg/axon/core/services/chat/internal/service"
	chatws "github.com/bvivg/axon/core/services/chat/internal/ws"
)

var version = "dev"

func main() {

	health.RunProbeIfRequested()

	if err := run(); err != nil {

		fmt.Fprintf(os.Stderr, "chat: %v\n", err)
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

	log.InfoContext(ctx, "starting chat",
		"version", version,
		"environment", string(cfg.Environment),
	)

	pool, err := postgres.Connect(ctx, cfg.Postgres, log)
	if err != nil {
		return err
	}
	defer pool.Close()

	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:             cfg.JWT.JWKSURL,
		RefreshInterval: cfg.JWT.JWKSRefreshInterval,
		Logger:          log,
	})
	if err != nil {
		return err
	}

	if err := keys.WaitUntilReady(ctx, authn.DefaultStartupTimeout); err != nil {
		return fmt.Errorf("initial jwks fetch: %w", err)
	}
	go keys.Start(ctx)

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     keys,
		Issuer:   cfg.JWT.Issuer,
		Audience: cfg.JWT.Audience,
	})
	if err != nil {
		return err
	}

	names, err := identity.New(identity.Config{
		HTTPClient: &http.Client{
			Timeout:   cfg.Auth.Timeout,
			Transport: internalTransport(),
		},
		BaseURL: cfg.Auth.ServiceURL,
		Timeout: cfg.Auth.Timeout,
		Logger:  log,
	})
	if err != nil {
		return err
	}

	publisher, closeBus, err := buildEvents(cfg, log)
	if err != nil {
		return err
	}
	defer closeBus()

	svc, err := service.New(service.Config{
		Store:  repository.New(pool),
		Events: publisher,
		Logger: log,
	})
	if err != nil {
		return err
	}

	cache, err := redis.Connect(ctx, cfg.Redis, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := cache.Close(); err != nil {
			log.Error("could not close the redis client", "error", err)
		}
	}()

	bus, err := pubsub.NewRedis(cache, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := bus.Close(); err != nil {
			log.Error("could not close the message bus", "error", err)
		}
	}()

	socket, err := chatws.New(chatws.Config{
		Service:  svc,
		Verifier: verifier,
		Bus:      bus,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	presenceSocket, err := chatpresence.New(chatpresence.Config{
		Verifier: verifier,
		Tracker:  presence.NewTracker(cache),
		Logger:   log,
	})
	if err != nil {
		return err
	}

	handler, err := server.New(server.Config{
		Service:  svc,
		Verifier: verifier,
		Names:    names,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(handler, socket, presenceSocket, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         unencryptedHTTP2(),
	}

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(pool, cache, metrics, log),
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

func buildEvents(cfg config.Config, log *slog.Logger) (service.Events, func(), error) {
	if len(cfg.Kafka.Brokers) == 0 {
		log.Warn("no kafka brokers configured; chat.message will not be published")
		return events.Discard(), func() {}, nil
	}

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers: cfg.Kafka.Brokers,
		Service: cfg.Service,
	}, log)
	if err != nil {
		return nil, nil, err
	}

	log.Info("publishing chat events", "brokers", cfg.Kafka.Brokers)

	return events.New(producer, log), func() {
		if err := producer.Close(); err != nil {
			log.Error("could not close the event producer", "error", err)
		}
	}, nil
}

func publicHandler(
	handler *server.Handler,
	socket *chatws.Handler,
	presenceSocket *chatpresence.Handler,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(chatws.Path, socket)
	mux.Handle(chatpresence.Path, presenceSocket)

	mux.Handle(chatv1connect.NewChatServiceHandler(handler,
		connect.WithInterceptors(
			middleware.NewCorrelationInterceptor(),
			middleware.NewRecoveryInterceptor(log),
			metrics.Interceptor(),
			middleware.NewLoggingInterceptor(log),
		),
	))

	return middleware.Chain(
		middleware.Correlation,
		middleware.Recovery(log),
		middleware.RequestLogger(log),
	)(mux)
}

func adminHandler(
	pool *postgres.Pool,
	cache *redis.Client,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	health.New(log, []health.Checker{pool, cache}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

func internalTransport() *http.Transport {
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

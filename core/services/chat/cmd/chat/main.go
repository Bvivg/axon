// Command chat runs the chat service.
//
// This file is wiring only: read configuration, build dependencies, start the
// servers, shut them down cleanly. Anything that decides how chat behaves lives
// under internal/.
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
	"github.com/bvivg/axon/core/shared/pkg/redis"

	"github.com/bvivg/axon/core/services/chat/internal/config"
	"github.com/bvivg/axon/core/services/chat/internal/events"
	"github.com/bvivg/axon/core/services/chat/internal/identity"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
	"github.com/bvivg/axon/core/services/chat/internal/server"
	"github.com/bvivg/axon/core/services/chat/internal/service"
	chatws "github.com/bvivg/axon/core/services/chat/internal/ws"
)

// version is stamped in at build time.
var version = "dev"

func main() {
	// Exits when started with -healthcheck. The runtime image is distroless and
	// has no shell for a container healthcheck to use, so the binary probes
	// itself.
	health.RunProbeIfRequested()

	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails, so this one
		// message goes to stderr directly.
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

	// Fetched once before serving, so a wrong URL or an auth service that never
	// came up fails at startup instead of as a wave of 401s.
	if err := keys.Refresh(ctx); err != nil {
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

	// Delivery between instances. Redis is required rather than optional: a
	// second instance without it looks healthy and quietly delivers nothing to
	// the people connected to the first one, which is worse than not starting.
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
		Handler:           publicHandler(handler, socket, metrics, log),
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

// buildEvents assembles the event publisher, or reports that there is no bus.
//
// No broker configured is a normal deployment rather than a broken one: nothing
// consumes chat.message yet, and messages reach the room over the socket
// regardless. Saying so out loud at startup is better than a nil that turns
// into a check at every send.
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

// publicHandler builds the contract listener.
//
// "Public" names the listener that carries the contract, not one the internet
// reaches: clients arrive through the gateway, which is where CORS, throttling
// and the policy live.
func publicHandler(
	handler *server.Handler,
	socket *chatws.Handler,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	// The socket carries no Connect interceptors: they are built around a call
	// that begins, answers and ends, and this one does none of those on the
	// schedule they assume. Correlation and recovery still apply, from the
	// HTTP chain below.
	mux.Handle(chatws.Path, socket)

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

// adminHandler builds the internal listener: probes and metrics.
func adminHandler(
	pool *postgres.Pool,
	cache *redis.Client,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	// Redis is in readiness, not only liveness: without it this instance still
	// answers, but nothing it is told reaches anybody connected elsewhere. An
	// instance that cannot deliver should not be taking sockets.
	health.New(log, []health.Checker{pool, cache}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

// internalTransport is the transport used for service-to-service calls: h2c,
// because gRPC needs HTTP/2 and there is no TLS inside the network to negotiate
// it with.
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

// unencryptedHTTP2 enables h2c alongside HTTP/1.1: gRPC clients need HTTP/2,
// while Connect's JSON and gRPC-Web transports use HTTP/1.1.
func unencryptedHTTP2() *http.Protocols {
	p := new(http.Protocols)
	p.SetHTTP1(true)
	p.SetUnencryptedHTTP2(true)
	return p
}

// serve runs one listener, reporting anything but a clean shutdown.
func serve(ctx context.Context, srv *http.Server, name string, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

// shutdown stops both listeners, giving in-flight requests a bounded chance to
// finish. The deadline is not optional: without it a single stuck request keeps
// the process alive until the orchestrator kills it.
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

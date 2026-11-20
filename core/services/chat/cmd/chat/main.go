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
	lksdk "github.com/livekit/server-sdk-go/v2"

	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/presence"
	"github.com/bvivg/axon/core/shared/pkg/redis"
	"github.com/bvivg/axon/core/shared/pkg/revocation"
	"github.com/bvivg/axon/core/shared/pkg/safehttp"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/config"
	"github.com/bvivg/axon/core/services/chat/internal/events"
	"github.com/bvivg/axon/core/services/chat/internal/identity"
	"github.com/bvivg/axon/core/services/chat/internal/media"
	"github.com/bvivg/axon/core/services/chat/internal/notify"
	chatpresence "github.com/bvivg/axon/core/services/chat/internal/presence"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
	"github.com/bvivg/axon/core/services/chat/internal/server"
	"github.com/bvivg/axon/core/services/chat/internal/service"
	"github.com/bvivg/axon/core/services/chat/internal/uploads"
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

	tlsIdentity, err := mtls.LoadEnv()
	if err != nil {
		return err
	}

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
		HTTPClient:      mtls.HTTPClient(tlsIdentity, 10*time.Second),
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
			Transport: mtls.Internal(tlsIdentity),
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

	if cfg.Calls.MigrationEnabled && !cfg.Calls.SFUEnabled {
		return errors.New("migration requires SFU")
	}
	repo := repository.NewWithOptions(pool, repository.Options{SFU: cfg.Calls.SFUEnabled, Migration: cfg.Calls.MigrationEnabled})

	cache, err := redis.Connect(ctx, cfg.Redis, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := cache.Close(); err != nil {
			log.Error("could not close the redis client", "error", err)
		}
	}()

	presenceTracker := presence.NewTracker(cache)

	listeners := events.Fanout{publisher}
	if cfg.Push.PublicKey != "" && cfg.Push.PrivateKey != "" {
		notifier, err := notify.New(notify.Config{
			Store:    repo,
			Presence: presenceTracker,
			Sender: notify.WebPush{
				PublicKey:  cfg.Push.PublicKey,
				PrivateKey: cfg.Push.PrivateKey,
				Subject:    cfg.Push.Subject,
				Client:     safehttp.NewClient(15 * time.Second),
			},
			Logger: log,
		})
		if err != nil {
			return err
		}
		go notifier.Run(ctx)
		listeners = append(listeners, notifier)
	} else {
		log.Warn("web push is off until CHAT_VAPID_PUBLIC_KEY and CHAT_VAPID_PRIVATE_KEY are set")
	}

	svc, err := service.New(service.Config{
		Store:  repo,
		Events: listeners,
		Logger: log,
	})
	if err != nil {
		return err
	}

	bus, err := pubsub.NewRedis(cache, log,
		pubsub.WithShards(cfg.Fanout.Shards),
		pubsub.WithMaxLen(cfg.Fanout.MaxLen),
		pubsub.WithBlock(cfg.Fanout.Block),
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := bus.Close(); err != nil {
			log.Error("could not close the message bus", "error", err)
		}
	}()

	revocationSubscriber, err := revocation.NewSubscriber(cache)
	if err != nil {
		return err
	}
	var callService *calls.Service
	var callHandler *server.CallsHandler
	var mediaHandler *server.MediaAdmissionHandler
	if cfg.Calls.Enabled {
		var media *calls.MediaProvider
		if cfg.Calls.SFUEnabled {
			if len(cfg.Calls.InternalKey.Reveal()) < 32 || len(cfg.Calls.MediaAPISecret.Reveal()) < 32 || cfg.Calls.MediaAPIKey == "" || cfg.Calls.MediaURL == "" || cfg.Calls.PublicMediaURL == "" {
				return errors.New("SFU credentials and URLs required")
			}
			media = &calls.MediaProvider{Rooms: lksdk.NewRoomServiceClient(cfg.Calls.MediaURL, cfg.Calls.MediaAPIKey, cfg.Calls.MediaAPISecret.Reveal()), APIKey: cfg.Calls.MediaAPIKey, APISecret: cfg.Calls.MediaAPISecret.Reveal(), PublicURL: cfg.Calls.PublicMediaURL}
		}
		callService, err = calls.NewService(calls.Config{MigrationEnabled: cfg.Calls.MigrationEnabled, DisableNewMigration: cfg.Calls.DisableNewMigration, Media: media, DisableNewSFU: cfg.Calls.DisableNewSFU, Store: repo, Families: names, Peers: names, Publisher: pubsub.CallsPublisher{Bus: bus}, Logger: log, TURNURLs: cfg.Calls.TURNURLs, TURNSecret: cfg.Calls.TURNSecret.Reveal()})
		if err != nil {
			return err
		}
		go callService.Run(ctx)
		if cfg.Push.PublicKey != "" && cfg.Push.PrivateKey != "" {
			callNotifier := notify.CallNotifier{Store: repo, Sender: notify.WebPush{PublicKey: cfg.Push.PublicKey, PrivateKey: cfg.Push.PrivateKey, Subject: cfg.Push.Subject, Client: safehttp.NewClient(3 * time.Second)}, Logger: log}
			go callNotifier.Run(ctx)
		}
		callHandler = server.NewCalls(callService, verifier, log)
		if cfg.Calls.MediaProbe || cfg.Calls.SFUEnabled {
			if len(cfg.Calls.InternalKey.Reveal()) < 32 || len(cfg.Calls.MediaAPISecret.Reveal()) < 32 || cfg.Calls.MediaAPIKey == "" {
				return errors.New("media admission credentials required")
			}
			mediaHandler = &server.MediaAdmissionHandler{Admission: &calls.MediaAdmission{AllowP2PProbe: cfg.Calls.MediaProbe, Store: repo, Families: names, APIKey: cfg.Calls.MediaAPIKey, APISecret: cfg.Calls.MediaAPISecret.Reveal(), InternalKey: cfg.Calls.InternalKey.Reveal()}}
			if cfg.Calls.MediaURL == "" {
				return errors.New("media API URL required")
			}
			mediaHandler.Reconciler = &calls.MediaReconciler{Admission: mediaHandler.Admission, Rooms: lksdk.NewRoomServiceClient(cfg.Calls.MediaURL, cfg.Calls.MediaAPIKey, cfg.Calls.MediaAPISecret.Reveal()), Log: log}
			if cfg.Calls.SFUEnabled {
				mediaHandler.Reconciler.Jobs = repo
			}
			go mediaHandler.Reconciler.Run(ctx)
		}
	}

	socket, err := chatws.New(chatws.Config{
		Service:     svc,
		Verifier:    verifier,
		Bus:         bus,
		Signals:     bus,
		UserSignals: bus,
		Names:       names,
		Revocation:  revocationSubscriber,
		Presence:    presenceTracker,
		Calls:       callService,
		Logger:      log,
	})
	if err != nil {
		return err
	}

	presenceSocket, err := chatpresence.New(chatpresence.Config{
		Verifier: verifier,
		Tracker:  presenceTracker,
		LastSeen: names,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	handler, err := server.New(server.Config{
		Service:  svc,
		Verifier: verifier,
		Names:    names,
		People:   names,
		Bus:      bus,
		Signals:  bus,
		Users:    bus,
		Logger:   log,

		PushPublicKey: cfg.Push.PublicKey,
	})
	if err != nil {
		return err
	}

	attachmentStore, err := attachment.NewStore(attachment.StoreConfig{
		Endpoint:  cfg.Attachments.Endpoint,
		AccessKey: cfg.Attachments.AccessKey,
		SecretKey: cfg.Attachments.SecretKey,
		Bucket:    cfg.Attachments.Bucket,
		UseSSL:    cfg.Attachments.UseSSL,
	})
	if err != nil {
		return err
	}
	attachmentURLs := attachment.NewURLBuilder(cfg.Attachments.PublicURL, cfg.Attachments.Bucket)
	if err := attachmentStore.EnsurePrivate(ctx); err != nil {
		return fmt.Errorf("make attachments private: %w", err)
	}

	sweeper, err := attachment.NewSweeper(attachment.SweeperConfig{
		Objects:     attachmentStore,
		Multipart:   attachmentStore,
		Uploads:     repo,
		Sessions:    repo,
		URLs:        attachmentURLs,
		Interval:    cfg.Attachments.SweepInterval,
		OrphanAfter: cfg.Attachments.OrphanAfter,
		Logger:      log,
	})
	if err != nil {
		return err
	}
	go sweeper.Run(ctx)

	mediaTools := media.NewTools(media.ToolsConfig{
		FFmpeg:  cfg.Media.FFmpeg,
		FFprobe: cfg.Media.FFprobe,
		Workers: cfg.Media.Workers,
		Timeout: cfg.Media.Timeout,
	})
	if err := mediaTools.Check(); err != nil {
		log.Warn("media processing is unavailable until ffmpeg is installed", "error", err)
	}

	pipeline := attachment.NewPipeline(attachmentStore, attachmentURLs, mediaTools)

	attachmentUpload, err := server.NewAttachmentHandler(server.AttachmentConfig{
		Verifier: verifier,
		Pipeline: pipeline,
		Recorder: svc,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	uploadManager, err := uploads.New(uploads.Config{
		Sessions:      repo,
		Objects:       attachmentStore,
		Processor:     pipeline,
		KeysOf:        sweeper.ObjectKeys,
		Logger:        log,
		MaxBytes:      cfg.Uploads.MaxBytes,
		MaxVoiceBytes: attachment.MaxVoiceBytes,
		PartSize:      cfg.Uploads.PartBytes,
		Workers:       cfg.Uploads.Workers,
	})
	if err != nil {
		return err
	}
	go uploadManager.Run(ctx)

	uploadSessions, err := server.NewUploadSessionsHandler(server.UploadSessionsConfig{
		Verifier: verifier,
		Manager:  uploadManager,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	mediaCheck := health.CheckerFunc{
		CheckerName: "media",
		Fn:          func(context.Context) error { return mediaTools.Check() },
	}
	checks := health.New(log, []health.Checker{pool, cache, mediaCheck})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.ShutdownTimeout, checks, log)
	publicSrv := graceful.Serve(&http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           withMediaAdmission(publicHandler(handler, callHandler, socket, presenceSocket, attachmentUpload, uploadSessions, metrics, log), mediaHandler),
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         mtls.ServerProtocols(tlsIdentity),
	})
	mediaMux := http.NewServeMux()
	mediaMux.Handle("/internal/media/", middleware.Chain(middleware.Correlation, middleware.Recovery(log), middleware.RequestLogger(log))(
		server.MediaDownload{Verifier: verifier, Access: repo, Families: names, Objects: attachmentStore},
	))
	mediaMux.Handle("/", publicSrv.Handler)
	publicSrv.Handler = mediaMux

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(checks, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 2)

	go serve(ctx, publicSrv, "public", tlsIdentity, log, errs)
	go serve(ctx, adminSrv, "admin", nil, log, errs)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	return graceful.Stop(publicSrv, adminSrv)
}

func withMediaAdmission(base http.Handler, handler *server.MediaAdmissionHandler) http.Handler {
	if handler == nil {
		return base
	}
	mux := http.NewServeMux()
	mux.Handle(callsv1connect.NewMediaAdmissionServiceHandler(handler))
	mux.HandleFunc("POST /internal/calls/livekit", handler.Reconciler.Webhook)
	mux.Handle("/", base)
	return mux
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
	callHandler *server.CallsHandler,
	socket *chatws.Handler,
	presenceSocket *chatpresence.Handler,
	attachmentUpload *server.AttachmentHandler,
	uploadSessions *server.UploadSessionsHandler,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(chatws.Path, socket)
	mux.Handle(chatpresence.Path, presenceSocket)
	mux.Handle("POST "+server.AttachmentUploadPath, attachmentUpload)
	uploadSessions.Register(mux)
	if callHandler != nil {
		mux.Handle(callsv1connect.NewCallsServiceHandler(callHandler, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), middleware.NewLoggingInterceptor(log))))
	}

	mux.Handle(chatv1connect.NewChatServiceHandler(handler,
		connect.WithInterceptors(
			telemetry.Interceptor(),
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
	checks *health.Handler,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	checks.Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	telemetry.Profiling(mux)

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

func serve(ctx context.Context, srv *http.Server, name string, tlsIdentity *mtls.Identity, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr, "mtls", tlsIdentity != nil)

	if err := mtls.ListenAndServe(srv, tlsIdentity); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

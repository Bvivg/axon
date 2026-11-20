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
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
	"github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/gateway/internal/attachmentproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/avatarproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/config"
	"github.com/bvivg/axon/core/services/gateway/internal/cookie"
	"github.com/bvivg/axon/core/services/gateway/internal/cors"
	"github.com/bvivg/axon/core/services/gateway/internal/guard"
	"github.com/bvivg/axon/core/services/gateway/internal/livekitproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/mediaproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/proxy"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
	"github.com/bvivg/axon/core/services/gateway/internal/wsproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/wstrace"
)

var version = "dev"

const sensitiveBurst = 5

const uploadsBurst = 30

const networkFactor = 10

func main() {

	health.RunProbeIfRequested()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gateway: %v\n", err)
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

	log.InfoContext(ctx, "starting gateway",
		"version", version,
		"environment", string(cfg.Environment),
		"allowed_origins", cfg.CORS.AllowedOrigins,
	)

	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:             cfg.JWKSURL,
		RefreshInterval: cfg.JWKSRefreshInterval,
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
		Issuer:   cfg.JWTIssuer,
		Audience: cfg.JWTAudience,
	})
	if err != nil {
		return err
	}

	var limits ratelimit.Store
	limitFailures := prometheus.NewCounter(prometheus.CounterOpts{Name: "gateway_rate_limit_store_errors_total", Help: "Rate limit checks that fell back to the local limiter."})
	if cfg.RateLimit.RedisAddr != "" {
		limitsClient := goredis.NewClient(&goredis.Options{Addr: cfg.RateLimit.RedisAddr, Password: cfg.RateLimit.RedisPassword, DialTimeout: time.Second, ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond})
		defer func() { _ = limitsClient.Close() }()
		limits = ratelimit.NewRedis(limitsClient)
	}
	limiter := func(name string, perMinute, burst int) *ratelimit.Limiter {
		return ratelimit.New(ratelimit.Config{Name: name, PerMinute: perMinute, Burst: burst, Store: limits, Failures: limitFailures})
	}
	search := limiter("search", cfg.RateLimit.SearchPerMinute, 0)
	standard := limiter("standard", cfg.RateLimit.StandardPerMinute, 0)
	sensitive := limiter("sensitive", cfg.RateLimit.SensitivePerMinute, sensitiveBurst)
	network := limiter("network", cfg.RateLimit.SensitivePerMinute*networkFactor, sensitiveBurst*networkFactor)
	uploads := limiter("uploads", cfg.RateLimit.UploadsPerMinute, uploadsBurst)
	uploadParts := limiter("upload-parts", cfg.RateLimit.UploadPartsPerMinute, 0)
	go search.Start(ctx)
	go standard.Start(ctx)
	go sensitive.Start(ctx)
	go network.Start(ctx)
	go uploads.Start(ctx)
	go uploadParts.Start(ctx)

	policyGuard, err := guard.New(guard.Config{
		Verifier:  verifier,
		Search:    search,
		Standard:  standard,
		Sensitive: sensitive,
		Network:   network,
		Logger:    log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)
	metrics.Registry().MustRegister(limitFailures)

	upstream := &http.Client{
		Timeout: cfg.UpstreamTimeout,

		Transport: internalTransport(),
	}

	authClient := proxy.NewClient(upstream, cfg.AuthServiceURL,
		connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor()),
	)

	authProxy, err := proxy.NewAuth(authClient)
	if err != nil {
		return err
	}

	chatClient := proxy.NewChatClient(upstream, cfg.ChatServiceURL,
		connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor()),
	)

	chatSocket, err := wsproxy.New(wsproxy.Config{
		SocketOptions: ws.Options{Compression: true, ReadLimit: ws.LargeMessageReadLimit},
		Upstream:      cfg.ChatSocketURL,
		Protocol:      chatSubprotocol,
		Verifier:      verifier,
		Origins:       cfg.CORS.AllowedOrigins,
		Logger:        log,
	})
	if err != nil {
		return err
	}

	gameProxy := proxy.NewGame(upstream, cfg.GameServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor()))
	gameSocket, err := wsproxy.New(wsproxy.Config{
		SocketOptions: ws.Options{ReadLimit: 4096},
		Upstream:      cfg.GameSocketURL, Protocol: "axon.game.v1", Verifier: verifier, Origins: cfg.CORS.AllowedOrigins, Logger: log,
		Inspect: wstrace.Game,
	})
	if err != nil {
		return err
	}

	presenceSocket, err := wsproxy.New(wsproxy.Config{
		Upstream: cfg.PresenceSocketURL,
		Protocol: presenceSubprotocol,
		Verifier: verifier,
		Origins:  cfg.CORS.AllowedOrigins,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	chatProxy, err := proxy.NewChat(chatClient)
	if err != nil {
		return err
	}
	callsProxy, err := proxy.NewCalls(upstream, cfg.ChatServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor()))
	if err != nil {
		return err
	}

	avatarUpload, err := avatarproxy.New(avatarproxy.Config{
		Upstream: cfg.AuthServiceURL + "/internal/avatar",
		Verifier: verifier,
		Limiter:  sensitive,
		Client:   upstream,
		Logger:   log,
	})
	if err != nil {
		return err
	}

	attachmentUpload, err := attachmentproxy.New(attachmentproxy.Config{
		Upstream: cfg.ChatServiceURL + "/internal/attachment",
		Verifier: verifier,
		Limiter:  uploads,
		Client: &http.Client{
			Timeout:   cfg.UploadTimeout,
			Transport: internalTransport(),
		},
		Logger: log,
	})
	if err != nil {
		return err
	}

	uploadSessions, err := attachmentproxy.NewSessions(attachmentproxy.SessionsConfig{
		Upstream:    cfg.ChatServiceURL + "/internal/uploads",
		Verifier:    verifier,
		Limiter:     uploads,
		PartLimiter: uploadParts,
		Client: &http.Client{
			Timeout:   cfg.UploadTimeout,
			Transport: internalTransport(),
		},
		Logger: log,
	})
	if err != nil {
		return err
	}

	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, authProxy, chatProxy, callsProxy, gameProxy, chatSocket, presenceSocket, gameSocket, avatarUpload, attachmentUpload, uploadSessions, policyGuard, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         unencryptedHTTP2(),
	}
	mediaDownload, err := mediaproxy.New(cfg.ChatServiceURL, verifier)
	if err != nil {
		return err
	}
	mediaMux := http.NewServeMux()
	mediaMux.Handle(authn.MediaPath+"/", cors.Middleware(cfg.CORS.AllowedOrigins)(mediaDownload))
	mediaMux.Handle("/", publicSrv.Handler)
	publicSrv.Handler = mediaMux
	if cfg.LiveKitURL != "" {
		mediaProxy, err := livekitproxy.New(livekitproxy.Config{Upstream: cfg.LiveKitURL, ChatURL: cfg.ChatServiceURL, InternalKey: cfg.CallsInternalKey.Reveal(), Origins: cfg.CORS.AllowedOrigins})
		if err != nil {
			return err
		}
		mux := http.NewServeMux()
		mux.Handle("/livekit/", cors.Middleware(cfg.CORS.AllowedOrigins)(mediaProxy))
		mux.Handle("/", publicSrv.Handler)
		publicSrv.Handler = mux
	}

	adminSrv := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           adminHandler(keys, metrics, log),
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

const (
	chatSocketPath       = "/ws/chat"
	chatSubprotocol      = "axon.chat.v1"
	presenceSocketPath   = "/ws/presence"
	presenceSubprotocol  = "axon.presence.v1"
	avatarUploadPath     = "/api/avatar"
	attachmentUploadPath = "/api/attachment"
	uploadSessionsPath   = "/api/uploads"
)

func publicHandler(
	cfg config.Config,
	authProxy authv1connect.AuthServiceHandler,
	chatProxy chatv1connect.ChatServiceHandler,
	callsProxy callsv1connect.CallsServiceHandler,
	gameProxy gamev1connect.GameServiceHandler,
	chatSocket http.Handler,
	presenceSocket http.Handler,
	gameSocket http.Handler,
	avatarUpload http.Handler,
	attachmentUpload http.Handler,
	uploadSessions *attachmentproxy.Sessions,
	policyGuard connect.Interceptor,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(chatSocketPath, chatSocket)
	mux.Handle(presenceSocketPath, presenceSocket)
	mux.Handle("/ws/game", gameSocket)
	mux.Handle("POST "+avatarUploadPath, avatarUpload)
	mux.Handle("POST "+attachmentUploadPath, attachmentUpload)
	uploadSessions.Register(mux, uploadSessionsPath)
	mux.Handle(callsv1connect.NewCallsServiceHandler(callsProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log))))
	mux.Handle(gamev1connect.NewGameServiceHandler(gameProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log))))

	mux.Handle(authv1connect.NewAuthServiceHandler(authProxy,
		connect.WithInterceptors(
			telemetry.Interceptor(),
			middleware.NewCorrelationInterceptor(),
			middleware.NewRecoveryInterceptor(log),
			metrics.Interceptor(),

			policyGuard,

			cookie.New(cookie.Config{
				Secure: cfg.RefreshCookie.Secure,
				MaxAge: cfg.RefreshCookie.MaxAge,
			}),
			middleware.NewLoggingInterceptor(log),
		),
	))

	mux.Handle(chatv1connect.NewChatServiceHandler(chatProxy,
		connect.WithInterceptors(
			telemetry.Interceptor(),
			middleware.NewCorrelationInterceptor(),
			middleware.NewRecoveryInterceptor(log),
			metrics.Interceptor(),
			policyGuard,
			middleware.NewLoggingInterceptor(log),
		),
	))

	return middleware.Chain(

		guard.ClientIPMiddleware(cfg.RateLimit.TrustedProxies),
		middleware.Correlation,
		middleware.Recovery(log),
		cors.Middleware(cfg.CORS.AllowedOrigins),
		middleware.RequestLogger(log),
	)(mux)
}

func adminHandler(keys *authn.Cache, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	health.New(log, []health.Checker{jwksChecker{keys}}).Register(mux)
	mux.Handle("/metrics", metrics.Handler())
	telemetry.Profiling(mux)

	return middleware.Chain(middleware.Correlation, middleware.Recovery(log))(mux)
}

type jwksChecker struct{ keys *authn.Cache }

func (c jwksChecker) Name() string { return "jwks" }

func (c jwksChecker) Check(context.Context) error {
	if len(c.keys.KeyIDs()) == 0 {
		return errors.New("no verification keys cached")
	}
	return nil
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

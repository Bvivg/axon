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

	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"

	"github.com/bvivg/axon/core/services/gateway/internal/avatarproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/config"
	"github.com/bvivg/axon/core/services/gateway/internal/cookie"
	"github.com/bvivg/axon/core/services/gateway/internal/cors"
	"github.com/bvivg/axon/core/services/gateway/internal/guard"
	"github.com/bvivg/axon/core/services/gateway/internal/proxy"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
	"github.com/bvivg/axon/core/services/gateway/internal/wsproxy"
)

var version = "dev"

const sensitiveBurst = 5

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

	standard := ratelimit.New(ratelimit.Config{PerMinute: cfg.RateLimit.StandardPerMinute})
	sensitive := ratelimit.New(ratelimit.Config{
		PerMinute: cfg.RateLimit.SensitivePerMinute,
		Burst:     sensitiveBurst,
	})
	go standard.Start(ctx)
	go sensitive.Start(ctx)

	policyGuard, err := guard.New(guard.Config{
		Verifier:  verifier,
		Standard:  standard,
		Sensitive: sensitive,
		Logger:    log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)

	upstream := &http.Client{
		Timeout: cfg.UpstreamTimeout,

		Transport: internalTransport(),
	}

	authClient := proxy.NewClient(upstream, cfg.AuthServiceURL,
		connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
	)

	authProxy, err := proxy.NewAuth(authClient)
	if err != nil {
		return err
	}

	chatClient := proxy.NewChatClient(upstream, cfg.ChatServiceURL,
		connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
	)

	chatSocket, err := wsproxy.New(wsproxy.Config{
		Upstream: cfg.ChatSocketURL,
		Protocol: chatSubprotocol,
		Verifier: verifier,
		Origins:  cfg.CORS.AllowedOrigins,
		Logger:   log,
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

	publicSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, authProxy, chatProxy, chatSocket, presenceSocket, avatarUpload, policyGuard, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		Protocols:         unencryptedHTTP2(),
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
	chatSocketPath      = "/ws/chat"
	chatSubprotocol     = "axon.chat.v1"
	presenceSocketPath  = "/ws/presence"
	presenceSubprotocol = "axon.presence.v1"
	avatarUploadPath    = "/api/avatar"
)

func publicHandler(
	cfg config.Config,
	authProxy authv1connect.AuthServiceHandler,
	chatProxy chatv1connect.ChatServiceHandler,
	chatSocket http.Handler,
	presenceSocket http.Handler,
	avatarUpload http.Handler,
	policyGuard connect.Interceptor,
	metrics *middleware.Metrics,
	log *slog.Logger,
) http.Handler {
	mux := http.NewServeMux()

	mux.Handle(chatSocketPath, chatSocket)
	mux.Handle(presenceSocketPath, presenceSocket)
	mux.Handle("POST "+avatarUploadPath, avatarUpload)

	mux.Handle(authv1connect.NewAuthServiceHandler(authProxy,
		connect.WithInterceptors(
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

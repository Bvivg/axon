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

	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/lifecycle"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
	"github.com/bvivg/axon/core/shared/pkg/resilience"
	"github.com/bvivg/axon/core/shared/pkg/rpclimit"
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
	"github.com/bvivg/axon/core/services/gateway/internal/reqlimit"
	"github.com/bvivg/axon/core/services/gateway/internal/wsproxy"
	"github.com/bvivg/axon/core/services/gateway/internal/wstrace"
)

var version = "dev"

const standardBurst = 60

const sensitiveBurst = 5

const adminBurst = 20

const uploadsBurst = 30

const networkFactor = 10

const socketsBurst = 30

var retryableProcedures = []string{
	gamev1connect.GameServiceGetSessionProcedure,
	gamev1connect.GameServiceListSessionsProcedure,
	gamev1connect.GameServiceGetMatchProcedure,
	gamev1connect.GameServiceGetRatingsProcedure,
	analysisv1connect.AnalysisServiceGetAnalysisProcedure,
	tournamentv1connect.TournamentServiceGetTournamentProcedure,
	tournamentv1connect.TournamentServiceListTournamentsProcedure,
	chatv1connect.ChatServiceListRoomsProcedure,
	chatv1connect.ChatServiceGetRoomProcedure,
	chatv1connect.ChatServiceListMessagesProcedure,
	chatv1connect.ChatServiceSearchMessagesProcedure,
	chatv1connect.ChatServiceListMessageReadersProcedure,
	authv1connect.AuthServiceGetMeProcedure,
	authv1connect.AuthServiceListSessionsProcedure,
	authv1connect.AuthServiceListRolesProcedure,
	authv1connect.AuthServiceSearchUsersProcedure,
	authv1connect.AuthServiceGetUsersPublicProfilesProcedure,
	authv1connect.AuthServiceGetPublicProfileByUsernameProcedure,
	callsv1connect.CallsServiceGetCallProcedure,
	callsv1connect.CallsServiceGetActiveCallProcedure,
	callsv1connect.CallsServiceListCallsProcedure,
	callsv1connect.CallsServiceGetCallCapabilitiesProcedure,
}

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

	identity, err := mtls.LoadEnv()
	if err != nil {
		return err
	}

	log.InfoContext(ctx, "starting gateway",
		"version", version,
		"environment", string(cfg.Environment),
		"allowed_origins", cfg.CORS.AllowedOrigins,
	)

	keys, err := authn.NewCache(authn.JWKSConfig{
		URL:             cfg.JWKSURL,
		HTTPClient:      mtls.HTTPClient(identity, 10*time.Second),
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
	standard := limiter("standard", cfg.RateLimit.StandardPerMinute, standardBurst)
	sensitive := limiter("sensitive", cfg.RateLimit.SensitivePerMinute, sensitiveBurst)
	adminWrites := limiter("admin", cfg.RateLimit.AdminPerMinute, adminBurst)
	network := limiter("network", cfg.RateLimit.SensitivePerMinute*networkFactor, sensitiveBurst*networkFactor)
	uploads := limiter("uploads", cfg.RateLimit.UploadsPerMinute, uploadsBurst)
	uploadParts := limiter("upload-parts", cfg.RateLimit.UploadPartsPerMinute, 0)
	sockets := limiter("sockets", cfg.RateLimit.SocketsPerMinute, socketsBurst)
	go sockets.Start(ctx)
	go search.Start(ctx)
	go standard.Start(ctx)
	go sensitive.Start(ctx)
	go adminWrites.Start(ctx)
	go network.Start(ctx)
	go uploads.Start(ctx)
	go uploadParts.Start(ctx)

	policyGuard, err := guard.New(guard.Config{
		Verifier:  verifier,
		Search:    search,
		Standard:  standard,
		Sensitive: sensitive,
		Admin:     adminWrites,
		Network:   network,
		Logger:    log,
	})
	if err != nil {
		return err
	}

	metrics := middleware.NewMetrics(cfg.Service)
	metrics.Registry().MustRegister(limitFailures)
	breakerTransitions := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gateway_circuit_transitions_total", Help: "Circuit breaker state changes per upstream procedure."}, []string{"procedure", "state"})
	metrics.Registry().MustRegister(breakerTransitions)
	guarded := func() *resilience.Interceptor {
		return resilience.New(resilience.Options{
			Retryable: resilience.Procedures(retryableProcedures...),
			OnState: func(procedure string, state resilience.State) {
				breakerTransitions.WithLabelValues(procedure, string(state)).Inc()
				log.Warn("circuit breaker changed state", "procedure", procedure, "state", string(state))
			},
		})
	}

	upstream := &http.Client{
		Timeout: cfg.UpstreamTimeout,

		Transport: mtls.Internal(identity),
	}

	authClient := proxy.NewClient(upstream, cfg.AuthServiceURL,
		connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()),
	)

	authProxy, err := proxy.NewAuth(authClient)
	if err != nil {
		return err
	}

	chatClient := proxy.NewChatClient(upstream, cfg.ChatServiceURL,
		connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()),
	)

	var socketClient *http.Client
	var mediaTransport http.RoundTripper
	if identity != nil {
		socketClient = &http.Client{Transport: identity.Transport(mtls.HTTP1Only())}
		mediaTransport = identity.Transport()
	}

	chatSocket, err := wsproxy.New(wsproxy.Config{
		HTTPClient:    socketClient,
		SocketOptions: ws.Options{Compression: true, ReadLimit: ws.LargeMessageReadLimit},
		Upstream:      cfg.ChatSocketURL,
		Protocol:      chatSubprotocol,
		Verifier:      verifier,
		Origins:       cfg.CORS.AllowedOrigins,
		Logger:        log,
		Limiter:       sockets,
		MaxPerUser:    cfg.RateLimit.SocketsPerUser,
	})
	if err != nil {
		return err
	}

	gameProxy := proxy.NewGame(upstream, cfg.GameServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()))
	analysisProxy := proxy.NewAnalysis(upstream, cfg.AnalysisServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()))
	tournamentProxy := proxy.NewTournament(upstream, cfg.TournamentServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()))
	gameSocket, err := wsproxy.New(wsproxy.Config{
		SocketOptions: ws.Options{ReadLimit: 4096},
		Upstream:      cfg.GameSocketURL, Protocol: "axon.game.v1", Verifier: verifier, Origins: cfg.CORS.AllowedOrigins, Logger: log,
		Inspect: wstrace.Game, HTTPClient: socketClient, Limiter: sockets, MaxPerUser: cfg.RateLimit.SocketsPerUser,
	})
	if err != nil {
		return err
	}

	presenceSocket, err := wsproxy.New(wsproxy.Config{
		HTTPClient: socketClient,
		Upstream:   cfg.PresenceSocketURL,
		Protocol:   presenceSubprotocol,
		Verifier:   verifier,
		Origins:    cfg.CORS.AllowedOrigins,
		Logger:     log,
		Limiter:    sockets,
		MaxPerUser: cfg.RateLimit.SocketsPerUser,
	})
	if err != nil {
		return err
	}

	chatProxy, err := proxy.NewChat(chatClient)
	if err != nil {
		return err
	}
	callsProxy, err := proxy.NewCalls(upstream, cfg.ChatServiceURL, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded()))
	if err != nil {
		return err
	}
	upstreamOptions := func() connect.ClientOption {
		return connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), guarded())
	}
	admin := adminProxies{
		auth:       proxy.NewAuthAdmin(upstream, cfg.AuthServiceURL, upstreamOptions()),
		game:       proxy.NewGameAdmin(upstream, cfg.GameServiceURL, upstreamOptions()),
		chat:       proxy.NewChatAdmin(upstream, cfg.ChatServiceURL, upstreamOptions()),
		calls:      proxy.NewCallsAdmin(upstream, cfg.ChatServiceURL, upstreamOptions()),
		rating:     proxy.NewRatingAdmin(upstream, cfg.RatingServiceURL, upstreamOptions()),
		tournament: proxy.NewTournamentAdmin(upstream, cfg.TournamentServiceURL, upstreamOptions()),
		analysis:   proxy.NewAnalysisAdmin(upstream, cfg.AnalysisServiceURL, upstreamOptions()),
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
			Transport: mtls.Internal(identity),
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
			Transport: mtls.Internal(identity),
		},
		Logger: log,
	})
	if err != nil {
		return err
	}

	checks := health.New(log, []health.Checker{jwksChecker{keys}})
	graceful := lifecycle.New(lifecycle.DrainFromEnv(), cfg.ShutdownTimeout, checks, log)
	publicSrv := graceful.Serve(&http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           publicHandler(cfg, authProxy, chatProxy, callsProxy, gameProxy, analysisProxy, tournamentProxy, admin, chatSocket, presenceSocket, gameSocket, avatarUpload, attachmentUpload, uploadSessions, policyGuard, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		Protocols:         mtls.ServerProtocols(nil),
	})
	mediaDownload, err := mediaproxy.New(cfg.ChatServiceURL, verifier, mediaTransport)
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
		Handler:           adminHandler(checks, metrics, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errs := make(chan error, 2)
	go serve(ctx, publicSrv, "public", nil, log, errs)
	go serve(ctx, adminSrv, "admin", nil, log, errs)

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	return graceful.Stop(publicSrv, adminSrv)
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

type adminProxies struct {
	auth       authv1connect.AuthAdminServiceHandler
	game       gamev1connect.GameAdminServiceHandler
	chat       chatv1connect.ChatAdminServiceHandler
	calls      callsv1connect.CallsAdminServiceHandler
	rating     ratingv1connect.RatingAdminServiceHandler
	tournament tournamentv1connect.TournamentAdminServiceHandler
	analysis   analysisv1connect.AnalysisAdminServiceHandler
}

func publicHandler(
	cfg config.Config,
	authProxy authv1connect.AuthServiceHandler,
	chatProxy chatv1connect.ChatServiceHandler,
	callsProxy callsv1connect.CallsServiceHandler,
	gameProxy gamev1connect.GameServiceHandler,
	analysisProxy analysisv1connect.AnalysisServiceHandler,
	tournamentProxy tournamentv1connect.TournamentServiceHandler,
	admin adminProxies,
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
	rpc := reqlimit.Route(reqlimit.Body(cfg.HTTP.MaxBodyBytes))
	upload := reqlimit.Deadline(cfg.UploadTimeout)
	edge := connect.WithHandlerOptions(rpclimit.Handler(cfg.RPCReadMaxBytes), connect.WithInterceptors(reqlimit.Lists(cfg.HTTP.MaxListItems)))

	mux.Handle(chatSocketPath, chatSocket)
	mux.Handle(presenceSocketPath, presenceSocket)
	mux.Handle("/ws/game", gameSocket)
	mux.Handle("POST "+avatarUploadPath, upload(avatarUpload))
	mux.Handle("POST "+attachmentUploadPath, upload(attachmentUpload))
	sessions := http.NewServeMux()
	uploadSessions.Register(sessions, uploadSessionsPath)
	mux.Handle(uploadSessionsPath, upload(sessions))
	mux.Handle(uploadSessionsPath+"/", upload(sessions))
	mux.Handle(rpc(callsv1connect.NewCallsServiceHandler(callsProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log)), edge)))
	mux.Handle(rpc(gamev1connect.NewGameServiceHandler(gameProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log)), edge)))
	mux.Handle(rpc(analysisv1connect.NewAnalysisServiceHandler(analysisProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log)), edge)))
	mux.Handle(rpc(tournamentv1connect.NewTournamentServiceHandler(tournamentProxy, connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log)), edge)))

	adminOptions := func() connect.HandlerOption {
		return connect.WithHandlerOptions(connect.WithInterceptors(telemetry.Interceptor(), middleware.NewCorrelationInterceptor(), middleware.NewRecoveryInterceptor(log), metrics.Interceptor(), policyGuard, middleware.NewLoggingInterceptor(log)), edge)
	}
	mux.Handle(rpc(authv1connect.NewAuthAdminServiceHandler(admin.auth, adminOptions())))
	mux.Handle(rpc(gamev1connect.NewGameAdminServiceHandler(admin.game, adminOptions())))
	mux.Handle(rpc(chatv1connect.NewChatAdminServiceHandler(admin.chat, adminOptions())))
	mux.Handle(rpc(callsv1connect.NewCallsAdminServiceHandler(admin.calls, adminOptions())))
	mux.Handle(rpc(ratingv1connect.NewRatingAdminServiceHandler(admin.rating, adminOptions())))
	mux.Handle(rpc(tournamentv1connect.NewTournamentAdminServiceHandler(admin.tournament, adminOptions())))
	mux.Handle(rpc(analysisv1connect.NewAnalysisAdminServiceHandler(admin.analysis, adminOptions())))

	mux.Handle(rpc(authv1connect.NewAuthServiceHandler(authProxy,
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
		edge,
	)))

	mux.Handle(rpc(chatv1connect.NewChatServiceHandler(chatProxy,
		connect.WithInterceptors(
			telemetry.Interceptor(),
			middleware.NewCorrelationInterceptor(),
			middleware.NewRecoveryInterceptor(log),
			metrics.Interceptor(),
			policyGuard,
			middleware.NewLoggingInterceptor(log),
		),
		edge,
	)))

	return middleware.Chain(

		guard.ClientIPMiddleware(cfg.RateLimit.TrustedProxies, log),
		middleware.Correlation,
		middleware.Recovery(log),
		cors.Middleware(cfg.CORS.AllowedOrigins),
		middleware.RequestLogger(log),
	)(mux)
}

func adminHandler(checks *health.Handler, metrics *middleware.Metrics, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	checks.Register(mux)
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

func serve(ctx context.Context, srv *http.Server, name string, identity *mtls.Identity, log *slog.Logger, errs chan<- error) {
	log.InfoContext(ctx, "listening", "listener", name, "addr", srv.Addr, "mtls", identity != nil)

	if err := mtls.ListenAndServe(srv, identity); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errs <- fmt.Errorf("%s listener: %w", name, err)
	}
}

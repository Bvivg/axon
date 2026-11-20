package config

import (
	"errors"
	"strings"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

const ServiceName = "gateway"

type Config struct {
	config.Base
	LiveKitURL       string
	CallsInternalKey config.Secret

	AuthServiceURL string

	ChatServiceURL string

	GameServiceURL string

	AnalysisServiceURL string

	GameSocketURL string

	ChatSocketURL string

	PresenceSocketURL string

	JWKSURL string

	JWKSRefreshInterval time.Duration

	JWTIssuer   string
	JWTAudience string

	CORS          CORSConfig
	RateLimit     RateLimitConfig
	RefreshCookie RefreshCookieConfig

	UpstreamTimeout time.Duration
	UploadTimeout   time.Duration
}

type CORSConfig struct {
	AllowedOrigins []string
}

const (
	chatSocketPath     = "/ws"
	presenceSocketPath = "/ws/presence"
)

func socketURL(base, path string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + path
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + path
	default:

		return strings.TrimSuffix(base, "/") + path
	}
}

type RateLimitConfig struct {
	RedisAddr string

	RedisPassword string

	SearchPerMinute int

	StandardPerMinute int

	SensitivePerMinute int

	UploadsPerMinute int

	UploadPartsPerMinute int

	TrustedProxies int
}

type RefreshCookieConfig struct {
	Secure bool

	MaxAge time.Duration
}

func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		LiveKitURL:          l.StringDefault("LIVEKIT_SIGNAL_URL", ""),
		CallsInternalKey:    l.SecretDefault("CALLS_INTERNAL_KEY", ""),
		Base:                config.LoadBase(l, ServiceName),
		AuthServiceURL:      l.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
		ChatServiceURL:      l.StringDefault("CHAT_SERVICE_URL", "http://chat:8082"),
		GameServiceURL:      l.StringDefault("GAME_SERVICE_URL", "http://game:8083"),
		AnalysisServiceURL:  l.StringDefault("ANALYSIS_SERVICE_URL", "http://analysis:8085"),
		JWKSURL:             l.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		JWKSRefreshInterval: l.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		UpstreamTimeout:     l.DurationDefault("UPSTREAM_TIMEOUT", 10*time.Second),
		UploadTimeout:       l.DurationDefault("UPLOAD_TIMEOUT", 4*time.Minute),
		CORS: CORSConfig{
			AllowedOrigins: l.StringSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
		},
		RateLimit: RateLimitConfig{
			RedisAddr:          l.StringDefault("RATE_LIMIT_REDIS_ADDR", ""),
			RedisPassword:      l.StringDefault("RATE_LIMIT_REDIS_PASSWORD", ""),
			SearchPerMinute:    l.IntDefault("RATE_LIMIT_SEARCH_PER_MINUTE", 60),
			StandardPerMinute:  l.IntDefault("RATE_LIMIT_PER_MINUTE", 120),
			SensitivePerMinute: l.IntDefault("RATE_LIMIT_AUTH_PER_MINUTE", 10),
			UploadsPerMinute:   l.IntDefault("RATE_LIMIT_UPLOADS_PER_MINUTE", 60),

			UploadPartsPerMinute: l.IntDefault("RATE_LIMIT_UPLOAD_PARTS_PER_MINUTE", 600),
			TrustedProxies:       l.IntDefault("TRUSTED_PROXIES", 0),
		},
	}

	cfg.JWTIssuer = l.StringDefault("JWT_ISSUER", "https://auth.axon.local")
	cfg.JWTAudience = l.StringDefault("JWT_AUDIENCE", "axon")

	cfg.RefreshCookie = RefreshCookieConfig{
		Secure: l.Bool("REFRESH_COOKIE_SECURE", cfg.Environment != config.EnvDevelopment),

		MaxAge: l.DurationDefault("REFRESH_COOKIE_MAX_AGE", 720*time.Hour),
	}

	if err := l.Err(); err != nil {
		return Config{}, err
	}

	cfg.ChatSocketURL = socketURL(cfg.ChatServiceURL, chatSocketPath)
	cfg.GameSocketURL = socketURL(cfg.GameServiceURL, "/ws")
	cfg.PresenceSocketURL = socketURL(cfg.ChatServiceURL, presenceSocketPath)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {

	for _, origin := range c.CORS.AllowedOrigins {
		if origin == "*" {
			return errors.New("config: CORS_ALLOWED_ORIGINS cannot contain '*': the gateway serves credentialed requests")
		}
	}

	if c.Environment.IsProduction() {
		if c.RateLimit.SearchPerMinute <= 0 || c.RateLimit.StandardPerMinute <= 0 || c.RateLimit.SensitivePerMinute <= 0 {
			return errors.New("config: rate limits must be positive in production")
		}
	}

	return nil
}

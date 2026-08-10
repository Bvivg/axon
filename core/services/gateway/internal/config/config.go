// Package config assembles the gateway's configuration from the environment.
package config

import (
	"errors"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

// ServiceName is what this service calls itself in logs and metrics.
const ServiceName = "gateway"

// Config is everything the gateway needs.
type Config struct {
	config.Base

	// AuthServiceURL is the base URL of the auth service's Connect listener.
	AuthServiceURL string

	// ChatServiceURL is the base URL of the chat service's Connect listener.
	ChatServiceURL string

	// JWKSURL is where the public key set is fetched from. It points at auth's
	// admin listener, which is internal — the document is public, but nothing
	// outside needs to reach it directly.
	JWKSURL string

	JWKSRefreshInterval time.Duration

	// JWTIssuer and JWTAudience must match what auth puts in a token, or every
	// verification fails for a reason that looks nothing like a configuration
	// mismatch.
	JWTIssuer   string
	JWTAudience string

	CORS          CORSConfig
	RateLimit     RateLimitConfig
	RefreshCookie RefreshCookieConfig

	// UpstreamTimeout bounds one forwarded call.
	UpstreamTimeout time.Duration
}

// CORSConfig configures the cross-origin policy.
type CORSConfig struct {
	// AllowedOrigins is an explicit list. A wildcard is not representable here,
	// which is the point: the gateway serves credentialed requests.
	AllowedOrigins []string
}

// RateLimitConfig configures the two budgets.
type RateLimitConfig struct {
	// StandardPerMinute applies to ordinary calls.
	StandardPerMinute int

	// SensitivePerMinute applies to sign-in, registration and refresh — the
	// operations worth brute-forcing.
	SensitivePerMinute int

	// TrustedProxies is how many proxy hops sit in front of the gateway.
	//
	// Zero means X-Forwarded-For is ignored entirely, which is correct when the
	// gateway is reached directly. Setting it higher than reality lets a caller
	// forge their own address and get a private rate limit bucket, so it must
	// match the deployment rather than be set optimistically.
	TrustedProxies int
}

// RefreshCookieConfig configures the cookie a browser holds its refresh token
// in. See internal/cookie for why the token goes there at all.
type RefreshCookieConfig struct {
	// Secure keeps the cookie off plaintext connections.
	Secure bool

	// MaxAge is how long the browser keeps it.
	MaxAge time.Duration
}

// Load reads the configuration, reporting every problem at once.
func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		Base:                config.LoadBase(l, ServiceName),
		AuthServiceURL:      l.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
		ChatServiceURL:      l.StringDefault("CHAT_SERVICE_URL", "http://chat:8082"),
		JWKSURL:             l.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
		JWKSRefreshInterval: l.DurationDefault("JWKS_REFRESH_INTERVAL", 5*time.Minute),
		UpstreamTimeout:     l.DurationDefault("UPSTREAM_TIMEOUT", 10*time.Second),
		CORS: CORSConfig{
			AllowedOrigins: l.StringSlice("CORS_ALLOWED_ORIGINS", []string{"http://localhost:3000"}),
		},
		RateLimit: RateLimitConfig{
			StandardPerMinute:  l.IntDefault("RATE_LIMIT_PER_MINUTE", 120),
			SensitivePerMinute: l.IntDefault("RATE_LIMIT_AUTH_PER_MINUTE", 10),
			TrustedProxies:     l.IntDefault("TRUSTED_PROXIES", 0),
		},
	}

	// Tokens are verified against auth's key set, so both sides have to agree on
	// who issued them and who they are for.
	cfg.JWTIssuer = l.StringDefault("JWT_ISSUER", "https://auth.axon.local")
	cfg.JWTAudience = l.StringDefault("JWT_AUDIENCE", "axon")

	// Secure is on unless this is a developer's machine. Reading the tier rather
	// than defaulting to false means forgetting the variable in production gives
	// the safe setting, not the convenient one.
	cfg.RefreshCookie = RefreshCookieConfig{
		Secure: l.Bool("REFRESH_COOKIE_SECURE", cfg.Environment != config.EnvDevelopment),
		// Matches the default refresh token lifetime in auth. The gateway cannot
		// read that service's configuration, so the two are kept in step by the
		// note in .env.example rather than by the type system.
		MaxAge: l.DurationDefault("REFRESH_COOKIE_MAX_AGE", 720*time.Hour),
	}

	if err := l.Err(); err != nil {
		return Config{}, err
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate catches configurations that parse but must not run.
func (c Config) validate() error {
	// A wildcard origin with credentials enabled is refused by browsers anyway,
	// but accepting it here would mean the intent was to serve every origin —
	// and on the trust boundary that intent is never right.
	for _, origin := range c.CORS.AllowedOrigins {
		if origin == "*" {
			return errors.New("config: CORS_ALLOWED_ORIGINS cannot contain '*': the gateway serves credentialed requests")
		}
	}

	// A production deployment with limiting switched off is almost certainly a
	// mistake, and one that is invisible until someone finds the login endpoint.
	if c.Environment.IsProduction() {
		if c.RateLimit.StandardPerMinute <= 0 || c.RateLimit.SensitivePerMinute <= 0 {
			return errors.New("config: rate limits must be positive in production")
		}
	}

	return nil
}

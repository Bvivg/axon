// Package config assembles the auth service's configuration from the
// environment. Everything is resolved at startup so a misconfigured service
// refuses to start rather than failing on the first request that needs the
// missing value.
package config

import (
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/redis"

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/password"
)

// ServiceName is what this service calls itself in logs and metrics.
const ServiceName = "auth"

// Config is everything the auth service needs.
type Config struct {
	config.Base

	Postgres postgres.Config
	Redis    redis.Config
	JWT      JWTConfig
	Password password.Params
	OAuth    OAuthConfig
}

// OAuthConfig configures provider sign-in.
type OAuthConfig struct {
	// RedirectBaseURL is the client route a provider returns the browser to.
	// The provider name is appended: .../auth/callback/google.
	//
	// It points at the web client, not at this service or the gateway. The
	// client reads the code and state out of the query and calls CompleteOAuth,
	// which is what the contract is shaped for, and means tokens never travel
	// through a redirect URL.
	RedirectBaseURL string

	// AllowedReturnOrigins is the allow-list for the post-sign-in destination.
	// An open redirect here is how an attacker harvests authorization codes.
	AllowedReturnOrigins []string

	Google oauth.ProviderConfig
	GitHub oauth.ProviderConfig

	// Apple needs four values rather than two: its client secret is an
	// assertion this service signs with the team's .p8 key, not a string an
	// operator copies out of a console.
	Apple oauth.AppleConfig

	// FakeEnabled turns on the local provider, which accepts any identity it is
	// given. Refused in production.
	FakeEnabled bool

	// FakeAuthorizeURL is the fake provider's authorize endpoint as a browser
	// would reach it. It is served on this service's public listener.
	FakeAuthorizeURL string
}

// JWTConfig configures token issuing and verification.
type JWTConfig struct {
	Issuer   string
	Audience string

	AccessTTL  time.Duration
	RefreshTTL time.Duration

	// Keys holds the signing key and every other key that stays verifiable.
	Keys *jwt.KeySet
}

// Load reads the configuration, reporting every problem it finds at once rather
// than making an operator restart the service to discover the next one.
func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		Base:     config.LoadBase(l, ServiceName),
		Postgres: postgres.LoadConfig(l),
		Redis:    redis.LoadConfig(l),
		JWT:      loadJWT(l),
		Password: loadPasswordParams(l),
		OAuth:    loadOAuth(l),
	}

	// The service owns one schema and reaches nothing else. Pinning the search
	// path here means a query does not depend on the role's default.
	if cfg.Postgres.SearchPath == "" {
		cfg.Postgres.SearchPath = ServiceName
	}

	if err := l.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadJWT(l *config.Loader) JWTConfig {
	cfg := JWTConfig{
		Issuer:     l.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
		Audience:   l.StringDefault("JWT_AUDIENCE", "axon"),
		AccessTTL:  l.DurationDefault("JWT_ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTTL: l.DurationDefault("JWT_REFRESH_TOKEN_TTL", 720*time.Hour),
	}

	activeID := l.String("JWT_ACTIVE_KEY_ID")
	if activeID == "" {
		// String already recorded the failure; without an id there is nothing
		// further to attempt.
		return cfg
	}

	active, ok := loadKey(l, activeID)
	if !ok {
		return cfg
	}

	// Retired keys stay published so tokens signed before a rotation keep
	// verifying until they expire.
	var retired []jwt.PrivateKey
	for _, id := range l.StringSlice("JWT_RETIRED_KEY_IDS", nil) {
		if key, ok := loadKey(l, id); ok {
			retired = append(retired, key)
		}
	}

	keys, err := jwt.NewKeySet(active, retired...)
	if err != nil {
		l.Fail("JWT_ACTIVE_KEY_ID", err)
		return cfg
	}

	cfg.Keys = keys
	return cfg
}

// loadKey reads the PEM for a key id, recording any problem on the loader so it
// joins the same batch as the rest of the configuration.
//
// The variable name is derived from the id, so adding a key during a rotation
// means adding an environment variable rather than editing this file: dev-1
// reads JWT_PRIVATE_KEY_DEV_1.
func loadKey(l *config.Loader, id string) (jwt.PrivateKey, bool) {
	varName := "JWT_PRIVATE_KEY_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(id))

	raw := l.Secret(varName)
	if raw.IsZero() {
		// Secret already recorded the missing variable.
		return jwt.PrivateKey{}, false
	}

	pemBytes, err := decodeKeyMaterial(raw.Reveal())
	if err != nil {
		l.Fail(varName, err)
		return jwt.PrivateKey{}, false
	}

	key, err := jwt.ParsePrivateKeyPEM(pemBytes)
	if err != nil {
		l.Fail(varName, err)
		return jwt.PrivateKey{}, false
	}

	return jwt.PrivateKey{ID: id, Key: key}, true
}

// decodeKeyMaterial accepts a PEM either literally or base64-encoded.
//
// Both exist because a PEM block is multi-line, and passing one through a .env
// file, a compose environment or a CI secret is inconsistent about newlines.
// Base64 sidesteps the whole question; a literal PEM still works for anyone
// mounting a file.
func decodeKeyMaterial(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)

	if strings.HasPrefix(trimmed, "-----BEGIN") {
		return []byte(trimmed), nil
	}

	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("key material is neither a PEM block nor base64")
	}
	return decoded, nil
}

// loadOAuth reads the provider settings.
//
// Nothing here is required. A deployment with no provider configured still
// serves the password flow, and the OAuth procedures answer "unsupported" — a
// service that refused to start because nobody had filled in a Google client id
// would be wrong about what it needs.
func loadOAuth(l *config.Loader) OAuthConfig {
	return OAuthConfig{
		RedirectBaseURL:      l.StringDefault("OAUTH_REDIRECT_BASE_URL", "http://localhost:3000/auth/callback"),
		AllowedReturnOrigins: l.StringSlice("OAUTH_ALLOWED_RETURN_ORIGINS", []string{"http://localhost:3000"}),
		Google: oauth.ProviderConfig{
			ClientID:     l.StringDefault("OAUTH_GOOGLE_CLIENT_ID", ""),
			ClientSecret: l.SecretDefault("OAUTH_GOOGLE_CLIENT_SECRET", "").Reveal(),
		},
		GitHub: oauth.ProviderConfig{
			ClientID:     l.StringDefault("OAUTH_GITHUB_CLIENT_ID", ""),
			ClientSecret: l.SecretDefault("OAUTH_GITHUB_CLIENT_SECRET", "").Reveal(),
		},
		Apple: oauth.AppleConfig{
			ClientID:   l.StringDefault("OAUTH_APPLE_CLIENT_ID", ""),
			TeamID:     l.StringDefault("OAUTH_APPLE_TEAM_ID", ""),
			KeyID:      l.StringDefault("OAUTH_APPLE_KEY_ID", ""),
			PrivateKey: applePrivateKey(l),
		},
		FakeEnabled:      l.Bool("OAUTH_FAKE_ENABLED", false),
		FakeAuthorizeURL: l.StringDefault("OAUTH_FAKE_AUTHORIZE_URL", "http://localhost:8081"+oauth.FakeAuthorizePath),
	}
}

// applePrivateKey reads the .p8 signing key Apple issues.
//
// Empty is normal and means Apple is simply not configured — the registry then
// treats it as absent, like any provider missing half its credentials. A value
// that is present but unreadable is the opposite case: somebody meant to
// configure Apple and got the encoding wrong, and starting anyway would hide
// that until the first person tried to sign in.
//
// Both a literal PEM and a base64-encoded one are accepted, for the same reason
// the JWT keys accept both: a PEM block is multi-line, and .env files, compose
// environments and CI secrets disagree about newlines.
func applePrivateKey(l *config.Loader) string {
	raw := l.SecretDefault("OAUTH_APPLE_PRIVATE_KEY", "").Reveal()
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	pemBytes, err := decodeKeyMaterial(raw)
	if err != nil {
		l.Fail("OAUTH_APPLE_PRIVATE_KEY", err)
		return ""
	}
	return string(pemBytes)
}

func loadPasswordParams(l *config.Loader) password.Params {
	defaults := password.DefaultParams()

	return password.Params{
		Memory:      uint32(l.IntDefault("ARGON2_MEMORY_KIB", int(defaults.Memory))),
		Time:        uint32(l.IntDefault("ARGON2_TIME", int(defaults.Time))),
		Parallelism: uint8(l.IntDefault("ARGON2_PARALLELISM", int(defaults.Parallelism))),
		SaltLength:  uint32(l.IntDefault("ARGON2_SALT_LENGTH", int(defaults.SaltLength))),
		KeyLength:   uint32(l.IntDefault("ARGON2_KEY_LENGTH", int(defaults.KeyLength))),
	}
}

// PublicKey exposes the active signing key's public half, for callers that need
// it outside the JWKS document.
func (c JWTConfig) PublicKey() *rsa.PublicKey {
	if c.Keys == nil {
		return nil
	}
	return &c.Keys.Active().Key.PublicKey
}

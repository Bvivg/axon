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

const ServiceName = "auth"

type Config struct {
	config.Base

	Postgres postgres.Config
	Redis    redis.Config
	JWT      JWTConfig
	Password password.Params
	OAuth    OAuthConfig
}

type OAuthConfig struct {
	RedirectBaseURL string

	AllowedReturnOrigins []string

	Google oauth.ProviderConfig
	GitHub oauth.ProviderConfig

	Apple oauth.AppleConfig

	FakeEnabled bool

	FakeAuthorizeURL string
}

type JWTConfig struct {
	Issuer   string
	Audience string

	AccessTTL  time.Duration
	RefreshTTL time.Duration

	Keys *jwt.KeySet
}

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

		return cfg
	}

	active, ok := loadKey(l, activeID)
	if !ok {
		return cfg
	}

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

func loadKey(l *config.Loader, id string) (jwt.PrivateKey, bool) {
	varName := "JWT_PRIVATE_KEY_" + strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(id))

	raw := l.Secret(varName)
	if raw.IsZero() {

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

func (c JWTConfig) PublicKey() *rsa.PublicKey {
	if c.Keys == nil {
		return nil
	}
	return &c.Keys.Active().Key.PublicKey
}

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

	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/password"
)

// ServiceName is what this service calls itself in logs and metrics.
const ServiceName = "auth"

// Config is everything the auth service needs.
type Config struct {
	config.Base

	Postgres postgres.Config
	JWT      JWTConfig
	Password password.Params
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
		JWT:      loadJWT(l),
		Password: loadPasswordParams(l),
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

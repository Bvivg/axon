// Package config assembles the chat service's configuration from the
// environment. Everything is resolved at startup so a misconfigured service
// refuses to start rather than failing on the first request that needs the
// missing value.
package config

import (
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

// ServiceName is what this service calls itself in logs and metrics.
const ServiceName = "chat"

// Config is everything the chat service needs.
type Config struct {
	config.Base

	Postgres postgres.Config
	JWT      JWTConfig
	Auth     AuthConfig
	Kafka    KafkaConfig
}

// KafkaConfig is where chat.message goes.
//
// An empty broker list is a valid configuration, not a broken one: the topic
// has no consumer yet, and a deployment without a broker still delivers
// messages to the people in the room. The service says so at startup and
// publishes nowhere.
type KafkaConfig struct {
	Brokers []string
}

// JWTConfig is what this service needs to verify a token itself.
//
// It verifies rather than trusts, even though every request has already passed
// the gateway: rules/security.md puts resource-level authorization in the
// service that owns the resource, and a service that believes a header because
// "the gateway must have checked" is one misrouted request from believing
// anyone.
type JWTConfig struct {
	// JWKSURL is where auth publishes its public keys, on auth's own internal
	// listener.
	JWKSURL string

	// JWKSRefreshInterval is how often the key set is re-fetched in the
	// background, so a rotation does not wait for a cache miss.
	JWKSRefreshInterval time.Duration

	Issuer   string
	Audience string
}

// AuthConfig is how this service reaches auth.
type AuthConfig struct {
	// ServiceURL is auth's Connect listener, on the internal network.
	//
	// It is used for exactly one thing: reading the caller's own profile, with
	// the caller's own token, to snapshot their display name when they join a
	// room. Chat has no user table and must not read auth's schema.
	ServiceURL string

	// Timeout bounds that call. It is short on purpose — the name is a nicety,
	// and joining a room must not hang because auth is slow.
	Timeout time.Duration
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		Base:     config.LoadBase(l, ServiceName),
		Postgres: postgres.LoadConfig(l),
		JWT: JWTConfig{
			JWKSURL:             l.StringDefault("JWKS_URL", "http://auth:9091/.well-known/jwks.json"),
			JWKSRefreshInterval: l.DurationDefault("JWKS_REFRESH_INTERVAL", 15*time.Minute),
			Issuer:              l.StringDefault("JWT_ISSUER", "https://auth.axon.local"),
			Audience:            l.StringDefault("JWT_AUDIENCE", "axon"),
		},
		Auth: AuthConfig{
			ServiceURL: l.StringDefault("AUTH_SERVICE_URL", "http://auth:8081"),
			Timeout:    l.DurationDefault("AUTH_SERVICE_TIMEOUT", 3*time.Second),
		},
		Kafka: KafkaConfig{
			Brokers: l.StringSlice("KAFKA_BROKERS", nil),
		},
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

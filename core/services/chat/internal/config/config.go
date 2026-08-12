package config

import (
	"time"

	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
	"github.com/bvivg/axon/core/shared/pkg/redis"
)

const ServiceName = "chat"

type Config struct {
	config.Base

	Postgres postgres.Config
	Redis    redis.Config
	JWT      JWTConfig
	Auth     AuthConfig
	Kafka    KafkaConfig
}

type KafkaConfig struct {
	Brokers []string
}

type JWTConfig struct {
	JWKSURL string

	JWKSRefreshInterval time.Duration

	Issuer   string
	Audience string
}

type AuthConfig struct {
	ServiceURL string

	Timeout time.Duration
}

func Load() (Config, error) {
	l := config.NewLoader()

	cfg := Config{
		Base:     config.LoadBase(l, ServiceName),
		Postgres: postgres.LoadConfig(l),
		Redis:    redis.LoadConfig(l),
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

	if cfg.Postgres.SearchPath == "" {
		cfg.Postgres.SearchPath = ServiceName
	}

	if err := l.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

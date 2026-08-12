package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

type Config struct {
	Addr string

	Password config.Secret

	DB int

	PoolSize int

	DialTimeout time.Duration

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

func (c *Config) applyDefaults() {
	if c.PoolSize == 0 {
		c.PoolSize = 10
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 3 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 3 * time.Second
	}
}

func LoadConfig(l *config.Loader) Config {
	return Config{
		Addr:         l.String("REDIS_ADDR"),
		Password:     l.SecretDefault("REDIS_PASSWORD", ""),
		DB:           l.IntDefault("REDIS_DB", 0),
		PoolSize:     l.IntDefault("REDIS_POOL_SIZE", 10),
		DialTimeout:  l.DurationDefault("REDIS_DIAL_TIMEOUT", 5*time.Second),
		ReadTimeout:  l.DurationDefault("REDIS_READ_TIMEOUT", 3*time.Second),
		WriteTimeout: l.DurationDefault("REDIS_WRITE_TIMEOUT", 3*time.Second),
	}
}

type Client struct {
	*redis.Client

	logger *slog.Logger
}

func Connect(ctx context.Context, cfg Config, log *slog.Logger) (*Client, error) {
	cfg.applyDefaults()

	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password.Reveal(),
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis at %s: %w", cfg.Addr, err)
	}

	log.InfoContext(ctx, "redis client ready", "addr", cfg.Addr, "db", cfg.DB)

	return &Client{Client: client, logger: log}, nil
}

func (c *Client) Name() string { return "redis" }

func (c *Client) Check(ctx context.Context) error {
	return c.Client.Ping(ctx).Err()
}

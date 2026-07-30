// Package redis builds the Redis client services use for caching, presence and
// pub/sub fan-out between replicas.
//
// Redis is never the source of truth in Axon. Everything held here — session
// state, presence, cached game boards — must be reconstructible from Postgres,
// so losing Redis costs latency, not data.
package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

// Config describes the client. Only Addr is required.
type Config struct {
	// Addr is host:port.
	Addr string
	// Password is empty in the local stack and set everywhere else.
	Password config.Secret
	// DB selects the logical database.
	DB int
	// PoolSize bounds concurrent connections.
	PoolSize int
	// DialTimeout bounds connection establishment.
	DialTimeout time.Duration
	// ReadTimeout and WriteTimeout bound individual commands. A blocked Redis
	// must surface as an error, not as a stuck request.
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

// LoadConfig reads the standard Redis variables.
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

// Client is a Redis client that also satisfies health.Checker.
type Client struct {
	*redis.Client

	logger *slog.Logger
}

// Connect opens the client and verifies it with a ping.
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

// Name implements health.Checker.
func (c *Client) Name() string { return "redis" }

// Check implements health.Checker.
func (c *Client) Check(ctx context.Context) error {
	return c.Client.Ping(ctx).Err()
}

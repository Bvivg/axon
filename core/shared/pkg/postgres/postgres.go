package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

type Config struct {
	DSN config.Secret

	SearchPath string

	MaxConns int32

	MinConns int32

	MaxConnLifetime time.Duration

	MaxConnIdleTime time.Duration

	ConnectTimeout time.Duration
}

func (c *Config) applyDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = 10
	}
	if c.MinConns == 0 {
		c.MinConns = 2
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
}

func LoadConfig(l *config.Loader) Config {
	return Config{
		DSN:             l.Secret("POSTGRES_DSN"),
		SearchPath:      l.StringDefault("POSTGRES_SEARCH_PATH", ""),
		MaxConns:        int32(l.IntDefault("POSTGRES_MAX_CONNS", 10)),
		MinConns:        int32(l.IntDefault("POSTGRES_MIN_CONNS", 2)),
		MaxConnLifetime: l.DurationDefault("POSTGRES_MAX_CONN_LIFETIME", time.Hour),
		MaxConnIdleTime: l.DurationDefault("POSTGRES_MAX_CONN_IDLE_TIME", 30*time.Minute),
		ConnectTimeout:  l.DurationDefault("POSTGRES_CONNECT_TIMEOUT", 5*time.Second),
	}
}

type Pool struct {
	*pgxpool.Pool

	logger *slog.Logger
}

func Connect(ctx context.Context, cfg Config, log *slog.Logger) (*Pool, error) {
	cfg.applyDefaults()

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN.Reveal())
	if err != nil {

		return nil, fmt.Errorf("parse POSTGRES_DSN: invalid connection string")
	}

	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	if cfg.SearchPath != "" {
		if poolCfg.ConnConfig.RuntimeParams == nil {
			poolCfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		poolCfg.ConnConfig.RuntimeParams["search_path"] = cfg.SearchPath
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	log.InfoContext(ctx, "postgres pool ready",
		"max_conns", cfg.MaxConns,
		"search_path", cfg.SearchPath,
	)

	return &Pool{Pool: pool, logger: log}, nil
}

func (p *Pool) Name() string { return "postgres" }

func (p *Pool) Check(ctx context.Context) error {
	return p.Ping(ctx)
}

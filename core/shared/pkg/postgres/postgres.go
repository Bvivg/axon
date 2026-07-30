// Package postgres builds the connection pool services use to reach Postgres.
//
// Postgres is the source of truth for every service that has durable state.
// Each service owns its own schema and its own migrations; nothing reaches
// across a schema boundary, so services stay independently deployable even
// though they share one database in the local stack.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

// Config describes the pool. Only DSN is required.
type Config struct {
	// DSN is the libpq/URL connection string.
	DSN config.Secret
	// SearchPath pins the service's schema, e.g. "auth". Set it so queries do
	// not depend on the role's default search_path.
	SearchPath string
	// MaxConns bounds the pool. Services get a modest default because the
	// database, not the service, is the scarce resource.
	MaxConns int32
	// MinConns keeps warm connections around.
	MinConns int32
	// MaxConnLifetime recycles connections so a long-lived pool eventually
	// picks up failovers and configuration changes.
	MaxConnLifetime time.Duration
	// MaxConnIdleTime closes connections nobody is using.
	MaxConnIdleTime time.Duration
	// ConnectTimeout bounds the initial handshake.
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

// LoadConfig reads the standard Postgres variables.
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

// Pool is a pgx pool that also satisfies health.Checker.
type Pool struct {
	*pgxpool.Pool

	logger *slog.Logger
}

// Connect opens the pool and verifies it with a ping, so a bad DSN fails at
// startup rather than on the first query.
func Connect(ctx context.Context, cfg Config, log *slog.Logger) (*Pool, error) {
	cfg.applyDefaults()

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN.Reveal())
	if err != nil {
		// The error from ParseConfig can echo the DSN, so it is not wrapped.
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

// Name implements health.Checker.
func (p *Pool) Name() string { return "postgres" }

// Check implements health.Checker.
func (p *Pool) Check(ctx context.Context) error {
	return p.Ping(ctx)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

type databases struct {
	primary postgres.Config
	replica *postgres.Config
	listen  *config.Secret
}

func loadDatabases(loader *config.Loader) databases {
	out := databases{primary: postgres.LoadConfig(loader)}
	if dsn := loader.SecretDefault("GAME_READ_DATABASE_URL", ""); dsn.Reveal() != "" {
		replica := out.primary
		replica.DSN = dsn
		out.replica = &replica
	}
	if dsn := loader.SecretDefault("GAME_LISTEN_DATABASE_URL", ""); dsn.Reveal() != "" {
		out.listen = &dsn
	}
	return out
}

func (d databases) listenConfig(fallback *pgx.ConnConfig) (*pgx.ConnConfig, error) {
	if d.listen == nil {
		return fallback, nil
	}
	parsed, err := pgx.ParseConfig(d.listen.Reveal())
	if err != nil {
		return nil, errors.New("parse GAME_LISTEN_DATABASE_URL: invalid connection string")
	}
	if d.primary.ConnectTimeout > 0 {
		parsed.ConnectTimeout = d.primary.ConnectTimeout
	}
	return parsed, nil
}

type connections struct {
	primary *postgres.Pool
	replica *postgres.Pool
	listen  *pgx.ConnConfig
}

func (d databases) connect(ctx context.Context, log *slog.Logger) (connections, error) {
	primary, err := postgres.Connect(ctx, d.primary, log)
	if err != nil {
		return connections{}, err
	}
	out := connections{primary: primary}
	if out.listen, err = d.listenConfig(primary.Config().ConnConfig); err != nil {
		primary.Close()
		return connections{}, err
	}
	if d.replica != nil {
		if out.replica, err = postgres.Connect(ctx, *d.replica, log.With("role", "read-replica")); err != nil {
			primary.Close()
			return connections{}, fmt.Errorf("read replica: %w", err)
		}
	}
	return out, nil
}

func (c connections) storeOptions() []session.Option {
	options := []session.Option{session.ListenVia(c.listen)}
	if c.replica != nil {
		options = append(options, session.ReadReplica(c.replica.Pool))
	}
	return options
}

func (c connections) Close() {
	if c.replica != nil {
		c.replica.Close()
	}
	c.primary.Close()
}

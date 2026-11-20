package main

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

const primaryDSN = "postgres://game_service:game@pgbouncer:6432/axon?sslmode=disable"

func TestDatabasesFallBackToPrimaryWhenUnset(t *testing.T) {
	loader := config.NewLoaderFromMap(map[string]string{"POSTGRES_DSN": primaryDSN, "GAME_READ_DATABASE_URL": "  "})
	dbs := loadDatabases(loader)
	if err := loader.Err(); err != nil {
		t.Fatal(err)
	}
	if dbs.primary.DSN.Reveal() != primaryDSN || dbs.replica != nil || dbs.listen != nil {
		t.Fatalf("unexpected databases: replica=%v listen=%v", dbs.replica != nil, dbs.listen != nil)
	}
	fallback, err := pgx.ParseConfig(primaryDSN)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dbs.listenConfig(fallback)
	if err != nil || got != fallback {
		t.Fatal("listen config must reuse the primary pool config", err)
	}
}

func TestDatabasesUseReplicaAndDirectListener(t *testing.T) {
	loader := config.NewLoaderFromMap(map[string]string{
		"POSTGRES_DSN":             primaryDSN,
		"POSTGRES_MAX_CONNS":       "7",
		"POSTGRES_CONNECT_TIMEOUT": "2s",
		"GAME_READ_DATABASE_URL":   "postgres://game_service:game@postgres-replica:5432/axon?sslmode=disable",
		"GAME_LISTEN_DATABASE_URL": "postgres://game_service:game@postgres:5432/axon?sslmode=disable",
	})
	dbs := loadDatabases(loader)
	if err := loader.Err(); err != nil {
		t.Fatal(err)
	}
	if dbs.replica == nil || dbs.replica.DSN.Reveal() != "postgres://game_service:game@postgres-replica:5432/axon?sslmode=disable" {
		t.Fatal("replica DSN not loaded")
	}
	if dbs.replica.MaxConns != 7 || dbs.replica.ConnectTimeout != 2*time.Second {
		t.Fatal("replica must inherit pool settings", dbs.replica.MaxConns, dbs.replica.ConnectTimeout)
	}
	if dbs.primary.DSN.Reveal() != primaryDSN {
		t.Fatal("primary DSN changed")
	}
	listen, err := dbs.listenConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if listen.Host != "postgres" || listen.Port != 5432 || listen.ConnectTimeout != 2*time.Second {
		t.Fatal("listen config", listen.Host, listen.Port, listen.ConnectTimeout)
	}
}

func TestDatabasesRejectBrokenListenerDSN(t *testing.T) {
	loader := config.NewLoaderFromMap(map[string]string{"POSTGRES_DSN": primaryDSN, "GAME_LISTEN_DATABASE_URL": "postgres://%zz"})
	if _, err := loadDatabases(loader).listenConfig(nil); err == nil {
		t.Fatal("broken listen DSN accepted")
	}
}

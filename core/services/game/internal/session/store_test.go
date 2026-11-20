package session

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func TestReplicaFallsBackToPrimary(t *testing.T) {
	primary := new(pgxpool.Pool)
	store := NewStore(primary, engine.NewRegistry())
	if store.Replica() != store {
		t.Fatal("store without a replica must serve history from the primary")
	}
}

func TestReplicaServesHistoryReads(t *testing.T) {
	primary, replica := new(pgxpool.Pool), new(pgxpool.Pool)
	listen := &pgx.ConnConfig{}
	games := engine.NewRegistry()
	store := NewStore(primary, games, ReadReplica(replica), ListenVia(listen))
	history := store.Replica()
	if history.pool != replica || history.games != games {
		t.Fatal("history store must read from the replica")
	}
	if store.pool != primary || store.listen != listen {
		t.Fatal("primary store changed")
	}
	if history.Replica() != history {
		t.Fatal("replica store must not chain further")
	}
}

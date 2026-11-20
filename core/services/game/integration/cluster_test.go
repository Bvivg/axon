//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/cluster"
	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

const partitions = 6

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type runner struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *runner) stop() {
	r.cancel()
	<-r.done
}

func spawn(run func(ctx context.Context)) *runner {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runner{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		run(ctx)
	}()
	return r
}

type node struct {
	*runner
	owner *owner.Owner
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func cell(i int) json.RawMessage {
	raw, _ := json.Marshal(tictactoe.Cell{Row: i / tictactoe.Size, Col: i % tictactoe.Size})
	return raw
}

func TestOwnershipFailoverKeepsEveryAcceptedMove(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"match_queue", "rating_changes", "moves", "sessions", "ratings", "owners", "projector_offsets"} {
		if _, err := pool.Exec(ctx, `DELETE FROM game.`+table); err != nil {
			t.Fatal(err)
		}
	}
	games := engine.NewRegistry()
	if err := games.Register(tictactoe.Definition()); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(pool, games)
	model := projection.New(store)
	rules := session.NewRules(games)
	cfg := journal.KafkaConfig{Brokers: brokers, Topic: "game.session-events.it", LeaseTopic: "game.partition-leases.it", Partitions: partitions, ReplicationFactor: 1}

	start := func(id string) *node {
		log, err := journal.NewKafka(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		own := owner.New(owner.Config{ID: id, Address: id + ":8083", Log: log, Model: model, Rules: rules, Now: time.Now})
		n := &node{owner: own}
		n.runner = spawn(func(ctx context.Context) {
			defer log.Close()
			_ = cluster.NewOwnership(cfg, own, quiet).Run(ctx)
		})
		t.Cleanup(n.stop)
		return n
	}
	project := func() *runner {
		r := spawn(func(ctx context.Context) { _ = cluster.NewProjector(cfg, model, quiet).Run(ctx) })
		t.Cleanup(r.stop)
		return r
	}
	projector := project()

	a := start("a")
	eventually(t, "a to own every partition", func() bool { return len(a.owner.Owned()) == partitions })
	b := start("b")
	eventually(t, "partitions to spread over both nodes", func() bool {
		owned := append(a.owner.Owned(), b.owner.Owned()...)
		slices.Sort(owned)
		return len(a.owner.Owned()) > 0 && len(b.owner.Owned()) > 0 && len(slices.Compact(owned)) == partitions && len(owned) == partitions
	})
	nodes := []*node{a, b}
	serving := func(id uuid.UUID) *owner.Owner {
		var found *owner.Owner
		eventually(t, "an owner for "+id.String(), func() bool {
			for _, n := range nodes {
				if n.owner.Serves(id) {
					found = n.owner
					return true
				}
			}
			return false
		})
		return found
	}

	type match struct {
		game    session.Session
		players [2]uuid.UUID
	}
	matches := make([]match, 12)
	moves := []int{0, 3, 1, 4, 2}
	for i := range matches {
		x, o := uuid.New(), uuid.New()
		id := session.SessionID(x, uuid.New())
		game, err := serving(id).Start(ctx, id, tictactoe.Key, x, o, true)
		if err != nil {
			t.Fatal(err)
		}
		matches[i] = match{game: game, players: [2]uuid.UUID{x, o}}
	}
	play := func(m *match, ply int) {
		t.Helper()
		next, err := serving(m.game.ID).Move(ctx, m.game.ID, m.players[ply%2], ply, tictactoe.Key, cell(moves[ply]), &m.game.Revision)
		if err != nil {
			t.Fatal(ply, err)
		}
		m.game = next
	}
	for i := range matches {
		play(&matches[i], 0)
		play(&matches[i], 1)
	}

	a.stop()
	nodes = []*node{b}
	eventually(t, "b to take over every partition", func() bool { return len(b.owner.Owned()) == partitions })
	for i := range matches {
		for ply := 2; ply < len(moves); ply++ {
			play(&matches[i], ply)
		}
		if matches[i].game.State.Status != engine.StatusWin {
			t.Fatal("game did not finish", matches[i].game.State)
		}
	}

	settled := func() bool {
		var finished, changes int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM game.sessions WHERE state->>'status' = 'win'`).Scan(&finished); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM game.rating_changes`).Scan(&changes); err != nil {
			t.Fatal(err)
		}
		return finished == len(matches) && changes == 2*len(matches)
	}
	eventually(t, "the projector to settle every game", settled)
	for _, m := range matches {
		stored, err := model.Load(ctx, m.game.ID)
		if err != nil || stored.Revision != m.game.Revision {
			t.Fatal("read model revision", stored.Revision, m.game.Revision, err)
		}
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM game.moves WHERE session_id=$1`, m.game.ID).Scan(&count); err != nil || count != len(moves) {
			t.Fatal("projected moves", count, err)
		}
	}

	projector.stop()
	var total int64
	if err := pool.QueryRow(ctx, `SELECT sum(next_offset) FROM game.projector_offsets`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE game.projector_offsets SET next_offset = GREATEST(next_offset - 3, 0)`); err != nil {
		t.Fatal(err)
	}
	project()
	eventually(t, "the restarted projector to catch up", func() bool {
		var now int64
		if err := pool.QueryRow(ctx, `SELECT sum(next_offset) FROM game.projector_offsets`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		return now == total
	})
	var played int
	if err := pool.QueryRow(ctx, `SELECT coalesce(max(games), 0) FROM game.ratings`).Scan(&played); err != nil || played != 1 {
		t.Fatal("a replayed projection settled a game twice", played, err)
	}
	if !settled() {
		t.Fatal("replay changed the read model")
	}
}

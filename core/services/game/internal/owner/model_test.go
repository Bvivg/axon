package owner_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

type model struct {
	mu       sync.Mutex
	log      *journal.Memory
	epochs   map[int32]int64
	offsets  map[int32]owner.Projection
	sessions map[uuid.UUID]session.Session
	moves    map[uuid.UUID]map[int]session.MoveRecord
}

func newModel(log *journal.Memory) *model {
	return &model{log: log, epochs: map[int32]int64{}, offsets: map[int32]owner.Projection{}, sessions: map[uuid.UUID]session.Session{}, moves: map[uuid.UUID]map[int]session.MoveRecord{}}
}

func (m *model) Claim(_ context.Context, partition int32, _, _ string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.epochs[partition]++
	return m.epochs[partition], nil
}

func (m *model) Projected(_ context.Context, partition int32) (owner.Projection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.offsets[partition], nil
}

func (m *model) Open(_ context.Context, partition, partitions int32) ([]session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var open []session.Session
	for id, game := range m.sessions {
		if journal.PartitionOf(id, partitions) == partition && (game.Opponent == nil || !game.State.Over()) {
			open = append(open, game)
		}
	}
	return open, nil
}

func (m *model) Load(_ context.Context, id uuid.UUID) (session.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	game, ok := m.sessions[id]
	if !ok {
		return session.Session{}, session.ErrNotFound
	}
	return game, nil
}

func (m *model) MoveBefore(_ context.Context, id uuid.UUID, ply int) (session.MoveRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	move, ok := m.moves[id][ply]
	return move, ok, nil
}

func (m *model) Moves(_ context.Context, id uuid.UUID, below int) ([]session.MoveRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []session.MoveRecord
	for ply, move := range m.moves[id] {
		if ply < below {
			out = append(out, move)
		}
	}
	slices.SortFunc(out, func(a, b session.MoveRecord) int { return a.Ply - b.Ply })
	return out, nil
}

func (m *model) project(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for partition := range m.log.Partitions() {
		end, _ := m.log.End(ctx, partition)
		m.mu.Lock()
		at := m.offsets[partition]
		fence := journal.NewFence()
		fence.Seed(partition, at.Generation)
		err := m.log.Read(ctx, partition, at.Offset, end, func(entry journal.Entry) error {
			if fence.Admit(entry) {
				m.apply(*entry.Event)
			}
			return nil
		})
		m.offsets[partition] = owner.Projection{Offset: end, Generation: fence.Generation(partition)}
		m.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func (m *model) apply(event journal.Event) {
	game, change := event.Session, event.Change
	if change.Fact == session.FactCancelled {
		delete(m.sessions, game.ID)
		delete(m.moves, game.ID)
		return
	}
	if current, ok := m.sessions[game.ID]; !ok || current.Revision < game.Revision {
		m.sessions[game.ID] = game
	}
	if m.moves[game.ID] == nil {
		m.moves[game.ID] = map[int]session.MoveRecord{}
	}
	switch change.Fact {
	case session.FactMoved:
		m.moves[game.ID][change.Move.Ply] = *change.Move
	case session.FactUndone:
		delete(m.moves[game.ID], change.UndonePly)
	}
}

type recorder struct {
	mu      sync.Mutex
	changed chan uuid.UUID
	moved   map[uuid.UUID]int
}

func newRecorder() *recorder {
	return &recorder{changed: make(chan uuid.UUID, 1024), moved: map[uuid.UUID]int{}}
}

func (r *recorder) Changed(id uuid.UUID) {
	select {
	case r.changed <- id:
	default:
	}
}

func (r *recorder) Moved(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moved[id]++
}

type cluster struct {
	log   *journal.Memory
	model *model
	rules session.Rules
	now   func() time.Time
}

func newCluster(t *testing.T, partitions int32) *cluster {
	t.Helper()
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	log := journal.NewMemory(partitions)
	return &cluster{log: log, model: newModel(log), rules: session.NewRules(games), now: time.Now}
}

func (c *cluster) node(t *testing.T, id string, listener owner.Listener, partitions ...int32) *owner.Owner {
	t.Helper()
	node := owner.New(owner.Config{ID: id, Address: id + ":8080", Log: c.log, Model: c.model, Rules: c.rules, Listener: listener, Now: func() time.Time { return c.now() }})
	for _, p := range partitions {
		if err := node.Assign(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(node.RevokeAll)
	return node
}

func all(partitions int32) []int32 {
	out := make([]int32, partitions)
	for i := range out {
		out[i] = int32(i)
	}
	return out
}

func cell(i int) json.RawMessage {
	raw, _ := json.Marshal(tictactoe.Cell{Row: i / tictactoe.Size, Col: i % tictactoe.Size})
	return raw
}

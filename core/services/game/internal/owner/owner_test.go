package owner_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func joined(t *testing.T, node *owner.Owner) (session.Session, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	x, o := uuid.New(), uuid.New()
	game, err := node.Create(ctx, x, uuid.New(), tictactoe.Key)
	if err != nil || game.Revision != 1 || game.Clock != nil {
		t.Fatal(game, err)
	}
	game, err = node.Join(ctx, game.ID, o)
	if err != nil || game.Revision != 2 || game.Clock == nil {
		t.Fatal(game, err)
	}
	return game, x, o
}

func play(t *testing.T, node *owner.Owner, game session.Session, user uuid.UUID, at int) session.Session {
	t.Helper()
	next, err := node.Move(context.Background(), game.ID, user, game.State.Ply, tictactoe.Key, cell(at), &game.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if next.Revision <= game.Revision {
		t.Fatal("revision did not advance")
	}
	return next
}

func TestCreateIsIdempotentPerRequest(t *testing.T) {
	c := newCluster(t, 4)
	node := c.node(t, "a", nil, all(4)...)
	ctx := context.Background()
	user, request := uuid.New(), uuid.New()
	first, err := node.Create(ctx, user, request, tictactoe.Key)
	if err != nil {
		t.Fatal(err)
	}
	again, err := node.Create(ctx, user, request, tictactoe.Key)
	if err != nil || again.ID != first.ID || again.Revision != first.Revision {
		t.Fatal(again, err)
	}
	if first.ID != session.SessionID(user, request) {
		t.Fatal("session id is not derived from the request")
	}
}

func TestUndoAndStaleRevisions(t *testing.T) {
	c := newCluster(t, 4)
	node := c.node(t, "a", nil, all(4)...)
	ctx := context.Background()
	game, x, o := joined(t, node)
	game = play(t, node, game, x, 0)
	old := game.Revision
	game = play(t, node, game, o, 4)
	game, err := node.Undo(ctx, game.ID, o, &game.Revision)
	if err != nil || game.State.Ply != 1 || game.State.Turn != 1 || game.Revision != 5 {
		t.Fatal(game, err)
	}
	if _, err := node.Move(ctx, game.ID, o, 1, tictactoe.Key, cell(8), &old); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal("stale move accepted", err)
	}
	if _, err := node.Undo(ctx, game.ID, x, &old); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal("stale undo accepted", err)
	}
	game = play(t, node, game, o, 8)
	if _, err := node.Undo(ctx, game.ID, uuid.New(), &game.Revision); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("outsider undo accepted", err)
	}
	read, err := node.Get(ctx, game.ID, x)
	if err != nil || read.Revision != game.Revision || read.State.Ply != 2 {
		t.Fatal(read, err)
	}
}

func TestUndoReplaysWhenSnapshotsAreMissing(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	game, x, o := joined(t, node)
	for ply, at := range []int{0, 3, 1, 4, 2} {
		user := x
		if ply%2 == 1 {
			user = o
		}
		game = play(t, node, game, user, at)
	}
	if !game.State.Over() {
		t.Fatal("not won")
	}
	c.model.project(t)
	node.Revoke(0)
	c.model.mu.Lock()
	for ply, move := range c.model.moves[game.ID] {
		move.StateBefore = engine.State{}
		move.ClockBefore = nil
		c.model.moves[game.ID][ply] = move
	}
	c.model.mu.Unlock()
	next := c.node(t, "b", nil, 0)
	game, err := next.Undo(ctx, game.ID, o, &game.Revision)
	if err != nil || game.State.Over() || game.State.Ply != 4 || game.State.Turn != 0 {
		t.Fatal(game, err)
	}
	game = play(t, next, game, x, 2)
	if !game.State.Over() {
		t.Fatal("replay lost earlier moves")
	}
}

func TestClockTimerFinishesTheGame(t *testing.T) {
	c := newCluster(t, 1)
	events := newRecorder()
	past := time.Now().Add(-6 * time.Minute)
	c.now = func() time.Time { return past }
	node := c.node(t, "a", events, 0)
	game, _, o := joined(t, node)
	c.now = time.Now
	deadline := time.After(2 * time.Second)
	for {
		read, err := node.Get(context.Background(), game.ID, o)
		if err != nil {
			t.Fatal(err)
		}
		if read.TimedOut {
			if read.State.Winner != 1 || read.Clock.RemainingMS[0] != 0 {
				t.Fatal(read)
			}
			return
		}
		select {
		case <-events.changed:
		case <-deadline:
			t.Fatal("clock never expired")
		}
	}
}

func TestConcurrentUndoAndMoveSerialize(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	game, x, o := joined(t, node)
	game = play(t, node, game, x, 0)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	wg.Go(func() { _, err := node.Undo(ctx, game.ID, x, &game.Revision); results <- err })
	wg.Go(func() {
		_, err := node.Move(ctx, game.ID, o, game.State.Ply, tictactoe.Key, cell(4), &game.Revision)
		results <- err
	})
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, engine.ErrStaleMove):
			stale++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatal(success, stale)
	}
}

func TestLeavingWaitingGameCancelsItAcrossHandover(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	user := uuid.New()
	game, err := node.Create(ctx, user, uuid.New(), tictactoe.Key)
	if err != nil {
		t.Fatal(err)
	}
	c.model.project(t)
	if _, cancelled, err := node.Leave(ctx, game.ID, user); err != nil || !cancelled {
		t.Fatal(cancelled, err)
	}
	node.Revoke(0)
	next := c.node(t, "b", nil, 0)
	if _, err := next.Get(ctx, game.ID, user); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("cancelled game came back", err)
	}
	c.model.project(t)
	if _, err := c.model.Load(ctx, game.ID); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("cancelled game is still projected", err)
	}
}

func TestRevokedOwnerRefusesAndNotifiesSockets(t *testing.T) {
	c := newCluster(t, 1)
	events := newRecorder()
	node := c.node(t, "a", events, 0)
	game, x, _ := joined(t, node)
	node.Revoke(0)
	if _, err := node.Get(context.Background(), game.ID, x); !errors.Is(err, owner.ErrNotOwner) {
		t.Fatal("revoked owner still serves", err)
	}
	events.mu.Lock()
	moved := events.moved[game.ID]
	events.mu.Unlock()
	if moved != 1 {
		t.Fatal("sockets were not told the game moved", moved)
	}
}

func TestNewOwnerRecoversUnprojectedEventsAndFencesZombie(t *testing.T) {
	c := newCluster(t, 1)
	ctx := context.Background()
	zombie := c.node(t, "a", nil, 0)
	game, x, o := joined(t, zombie)
	game = play(t, zombie, game, x, 0)
	successor := c.node(t, "b", nil, 0)
	if _, err := zombie.Move(ctx, game.ID, o, game.State.Ply, tictactoe.Key, cell(8), &game.Revision); err != nil {
		t.Fatal("zombie still appends in this model", err)
	}
	read, err := successor.Get(ctx, game.ID, o)
	if err != nil || read.Revision != game.Revision || read.State.Ply != 1 {
		t.Fatal("successor lost or absorbed events", read, err)
	}
	next := play(t, successor, read, o, 4)
	c.model.project(t)
	projected, err := c.model.Load(ctx, game.ID)
	if err != nil || projected.Revision != next.Revision || projected.State.Ply != 2 {
		t.Fatal("projection admitted the zombie write", projected, err)
	}
	if _, ok := c.model.moves[game.ID][2]; !ok {
		t.Fatal("successor move missing")
	}
}

func TestAppendFailureLeavesStateUnchanged(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	game, x, _ := joined(t, node)
	broken := errors.New("broker down")
	c.log.FailWith(func(int32) error { return broken })
	if _, err := node.Move(ctx, game.ID, x, game.State.Ply, tictactoe.Key, cell(0), &game.Revision); !errors.Is(err, broken) {
		t.Fatal(err)
	}
	c.log.FailWith(nil)
	read, err := node.Get(ctx, game.ID, x)
	if err != nil || read.Revision != game.Revision || read.State.Ply != 0 {
		t.Fatal("failed append changed the game", read, err)
	}
	play(t, node, read, x, 0)
}

func TestSimulatedHandoversNeverLoseAcceptedCommands(t *testing.T) {
	for seed := range uint64(20) {
		simulate(t, seed)
	}
}

func simulate(t *testing.T, seed uint64) {
	const partitions, nodes, games, steps = 4, 3, 12, 400
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	c := newCluster(t, partitions)
	ctx := context.Background()
	cluster := make([]*owner.Owner, nodes)
	for i := range cluster {
		cluster[i] = c.node(t, string(rune('a'+i)), nil)
	}
	holder := map[int32]int{}
	for p := range int32(partitions) {
		holder[p] = int(p) % nodes
		if err := cluster[holder[p]].Assign(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	serving := func(id uuid.UUID) *owner.Owner { return cluster[holder[journal.PartitionOf(id, partitions)]] }
	type tracked struct {
		game    session.Session
		players [2]uuid.UUID
	}
	expected := make([]tracked, games)
	for i := range expected {
		x, o := uuid.New(), uuid.New()
		id := session.SessionID(x, uuid.New())
		node := serving(id)
		game, err := node.Start(ctx, id, tictactoe.Key, x, o, false)
		if err != nil {
			t.Fatal(err)
		}
		expected[i] = tracked{game: game, players: [2]uuid.UUID{x, o}}
	}
	for step := range steps {
		switch rng.IntN(10) {
		case 0:
			p := int32(rng.IntN(partitions))
			cluster[holder[p]].Revoke(p)
			holder[p] = (holder[p] + 1 + rng.IntN(nodes-1)) % nodes
			if err := cluster[holder[p]].Assign(ctx, p); err != nil {
				t.Fatal(seed, step, err)
			}
		case 1:
			c.model.project(t)
		case 2:
			item := &expected[rng.IntN(games)]
			if item.game.State.Ply == 0 {
				continue
			}
			next, err := serving(item.game.ID).Undo(ctx, item.game.ID, item.players[rng.IntN(2)], &item.game.Revision)
			if err != nil {
				t.Fatal(seed, step, "undo", err)
			}
			item.game = next
		default:
			item := &expected[rng.IntN(games)]
			if item.game.State.Over() {
				continue
			}
			player := item.players[item.game.State.Turn]
			cells, _, err := tictactoe.Position(item.game.State)
			if err != nil {
				t.Fatal(err)
			}
			free := []int{}
			for i, mark := range cells {
				if mark < 0 {
					free = append(free, i)
				}
			}
			next, err := serving(item.game.ID).Move(ctx, item.game.ID, player, item.game.State.Ply, tictactoe.Key, cell(free[rng.IntN(len(free))]), &item.game.Revision)
			if err != nil {
				t.Fatal(seed, step, "move", err)
			}
			item.game = next
		}
	}
	c.model.project(t)
	fresh := c.node(t, "fresh", nil)
	for _, n := range cluster {
		n.RevokeAll()
	}
	for p := range int32(partitions) {
		if err := fresh.Assign(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range expected {
		projected, err := c.model.Load(ctx, item.game.ID)
		if err != nil || projected.Revision != item.game.Revision || projected.State.Ply != item.game.State.Ply {
			t.Fatalf("seed %d: projection diverged for %s: %+v vs %+v (%v)", seed, item.game.ID, projected.Revision, item.game.Revision, err)
		}
		read, err := fresh.Get(ctx, item.game.ID, item.players[0])
		if err != nil || read.Revision != item.game.Revision || string(read.State.Data) != string(item.game.State.Data) {
			t.Fatalf("seed %d: recovered owner diverged for %s (%v)", seed, item.game.ID, err)
		}
	}
}

func TestSuccessorWaitsForTheLeaseAndTheZombieStopsAcking(t *testing.T) {
	c := newCluster(t, 1)
	ctx := context.Background()
	lease := 400 * time.Millisecond
	zombie := c.leased(t, "a", nil, lease, 0)
	game, x, o := joined(t, zombie)
	game = play(t, zombie, game, x, 0)
	started := time.Now()
	successor := c.leased(t, "b", nil, lease, 0)
	if waited := time.Since(started); waited < lease/2 {
		t.Fatal("successor served before the previous lease expired", waited)
	}
	if _, err := zombie.Move(ctx, game.ID, o, game.State.Ply, tictactoe.Key, cell(8), &game.Revision); !errors.Is(err, owner.ErrNotOwner) {
		t.Fatal("zombie acknowledged a move after its lease expired", err)
	}
	read, err := successor.Get(ctx, game.ID, o)
	if err != nil || read.Revision != game.Revision {
		t.Fatal(read, err)
	}
	play(t, successor, read, o, 4)
}

func TestRenewedLeaseKeepsServing(t *testing.T) {
	c := newCluster(t, 1)
	lease := 300 * time.Millisecond
	node := c.leased(t, "a", nil, lease, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go node.KeepLeases(ctx)
	game, x, _ := joined(t, node)
	time.Sleep(3 * lease)
	play(t, node, game, x, 0)
}

func TestLostLeaseRevokesThePartition(t *testing.T) {
	c := newCluster(t, 1)
	events := newRecorder()
	lease := 300 * time.Millisecond
	node := c.leased(t, "a", events, lease, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go node.KeepLeases(ctx)
	game, x, _ := joined(t, node)
	if _, err := c.model.Claim(ctx, 0, "b", "b:8080", lease); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * lease)
	for node.Serving(0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := node.Get(ctx, game.ID, x); !errors.Is(err, owner.ErrNotOwner) {
		t.Fatal("owner kept serving a partition claimed by someone else", err)
	}
	events.mu.Lock()
	defer events.mu.Unlock()
	if events.moved[game.ID] != 1 {
		t.Fatal("sockets were not moved off the lost partition")
	}
}

func TestLostAppendAckRecoversThePersistedRecord(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	game, x, o := joined(t, node)
	lost := errors.New("ack lost")
	c.log.LoseAcksWith(func(int32) error { return lost })
	if _, err := node.Move(ctx, game.ID, x, game.State.Ply, tictactoe.Key, cell(0), &game.Revision); !errors.Is(err, lost) {
		t.Fatal(err)
	}
	c.log.LoseAcksWith(nil)
	read, err := node.Get(ctx, game.ID, o)
	if err != nil || read.Revision != game.Revision+1 || read.State.Ply != 1 {
		t.Fatal("owner served a state behind the journal", read.Revision, read.State.Ply, err)
	}
	if _, err := node.Move(ctx, game.ID, x, game.State.Ply, tictactoe.Key, cell(4), &game.Revision); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal("stale move accepted after a lost ack", err)
	}
	next := play(t, node, read, o, 4)
	c.model.project(t)
	projected, err := c.model.Load(ctx, game.ID)
	if err != nil || projected.Revision != next.Revision || string(projected.State.Data) != string(next.State.Data) {
		t.Fatal("projection diverged from the owner", projected.Revision, next.Revision, err)
	}
}

func TestLostAckOnCreateDoesNotCreateTwice(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	ctx := context.Background()
	user, request := uuid.New(), uuid.New()
	c.log.LoseAcksWith(func(int32) error { return errors.New("ack lost") })
	if _, err := node.Create(ctx, user, request, tictactoe.Key); err == nil {
		t.Fatal("lost ack reported success")
	}
	c.log.LoseAcksWith(nil)
	again, err := node.Create(ctx, user, request, tictactoe.Key)
	if err != nil || again.Revision != 1 {
		t.Fatal(again, err)
	}
	end, _ := c.log.End(ctx, 0)
	created := 0
	_ = c.log.Read(ctx, 0, 0, end, func(entry journal.Entry) error {
		if entry.Event != nil && entry.Event.Session.ID == again.ID {
			created++
		}
		return nil
	})
	if created != 1 {
		t.Fatal("session created twice", created)
	}
}

func TestCancelledCallerDoesNotAbortTheAppend(t *testing.T) {
	c := newCluster(t, 1)
	node := c.node(t, "a", nil, 0)
	game, x, o := joined(t, node)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	moved, err := node.Move(gone, game.ID, x, game.State.Ply, tictactoe.Key, cell(0), &game.Revision)
	if err != nil || moved.Revision != game.Revision+1 {
		t.Fatal("append followed the caller's context", err)
	}
	play(t, node, moved, o, 4)
	c.model.project(t)
}

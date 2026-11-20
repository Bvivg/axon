package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

type fakeQueue struct {
	polls   atomic.Int64
	touches atomic.Int64
	mu      sync.Mutex
	fail    error
	changes updates
}

func (q *fakeQueue) poll(_ context.Context, key matchKey, touch bool) (*gamev1.MatchTicket, error) {
	q.polls.Add(1)
	if touch {
		q.touches.Add(1)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.fail != nil {
		return nil, q.fail
	}
	return &gamev1.MatchTicket{Game: string(key.game), Status: gamev1.MatchStatus_MATCH_STATUS_SEARCHING}, nil
}

func (q *fakeQueue) watch(user uuid.UUID) (<-chan struct{}, func()) {
	changed, _, unsubscribe := q.changes.subscribe(user)
	return changed, unsubscribe
}

func newFakeFeeds(limit int, interval time.Duration) (*matchFeeds, *fakeQueue) {
	queue := &fakeQueue{changes: newUpdates()}
	return newMatchFeeds(limit, interval, queue.poll, queue.watch), queue
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func receive(t *testing.T, listener *matchListener) (*gamev1.MatchTicket, error) {
	t.Helper()
	select {
	case <-listener.signal:
		return listener.latest()
	case <-time.After(5 * time.Second):
		t.Fatal("no ticket arrived")
		return nil, nil
	}
}

func TestMatchSocketsOfOneUserShareOnePoller(t *testing.T) {
	feeds, queue := newFakeFeeds(100, 20*time.Millisecond)
	key := matchKey{user: uuid.New(), game: "chess"}
	listeners := make([]*matchListener, 50)
	for i := range listeners {
		listener, ok := feeds.join(key)
		if !ok {
			t.Fatalf("socket %d refused under the limit", i)
		}
		listeners[i] = listener
	}
	for _, listener := range listeners {
		if ticket, err := receive(t, listener); err != nil || ticket.GetGame() != "chess" {
			t.Fatalf("ticket = %v, %v", ticket, err)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if polls := queue.polls.Load(); polls > 20 {
		t.Fatalf("50 sockets ran %d queue polls in 200ms, want one poller (about 10)", polls)
	}
	for _, listener := range listeners {
		listener.leave()
	}
	waitFor(t, "the poller to stop", func() bool { return feeds.active() == 0 })
	stopped := queue.polls.Load()
	time.Sleep(100 * time.Millisecond)
	if queue.polls.Load() != stopped {
		t.Fatal("the poller kept polling after the last socket left")
	}
}

func TestMatchSocketsArePerUserLimited(t *testing.T) {
	feeds, _ := newFakeFeeds(2, time.Hour)
	user := uuid.New()
	first, ok := feeds.join(matchKey{user: user, game: "chess"})
	if !ok {
		t.Fatal("first socket refused")
	}
	if _, ok := feeds.join(matchKey{user: user, game: "quoridor"}); !ok {
		t.Fatal("second socket refused")
	}
	if _, ok := feeds.join(matchKey{user: user, game: "chess"}); ok {
		t.Fatal("a third socket of the same user was accepted")
	}
	if _, ok := feeds.join(matchKey{user: uuid.New(), game: "chess"}); !ok {
		t.Fatal("the limit leaked to another user")
	}
	first.leave()
	first.leave()
	if _, ok := feeds.join(matchKey{user: user, game: "chess"}); !ok {
		t.Fatal("a freed slot was not reusable")
	}
	if _, ok := feeds.join(matchKey{user: user, game: "chess"}); ok {
		t.Fatal("leaving twice freed two slots")
	}
}

func TestMatchChangesRefreshWithoutTouchingTheQueue(t *testing.T) {
	feeds, queue := newFakeFeeds(4, time.Hour)
	user := uuid.New()
	listener, _ := feeds.join(matchKey{user: user, game: "chess"})
	defer listener.leave()
	if _, err := receive(t, listener); err != nil {
		t.Fatal(err)
	}
	queue.changes.notify(user)
	if _, err := receive(t, listener); err != nil {
		t.Fatal(err)
	}
	if queue.polls.Load() != 2 || queue.touches.Load() != 1 {
		t.Fatalf("polls %d touches %d, want 2 and 1", queue.polls.Load(), queue.touches.Load())
	}
}

func TestAFailedPollClosesTheFeedAndTheNextSocketStartsAnother(t *testing.T) {
	feeds, queue := newFakeFeeds(4, time.Hour)
	queue.fail = errors.New("database down")
	key := matchKey{user: uuid.New(), game: engine.Key("chess")}
	broken, _ := feeds.join(key)
	if _, err := receive(t, broken); err == nil {
		t.Fatal("a failed poll reached the socket as a ticket")
	}
	queue.mu.Lock()
	queue.fail = nil
	queue.mu.Unlock()
	fresh, _ := feeds.join(key)
	if ticket, err := receive(t, fresh); err != nil || ticket == nil {
		t.Fatalf("ticket after recovery = %v, %v", ticket, err)
	}
	broken.leave()
	fresh.leave()
	waitFor(t, "every poller to stop", func() bool { return feeds.active() == 0 })
}

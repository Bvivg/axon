package server

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const (
	DefaultMatchSockets = 8
	matchPollInterval   = 2 * time.Second
)

type matchKey struct {
	user uuid.UUID
	game engine.Key
}

type matchPoll func(ctx context.Context, key matchKey, touch bool) (*gamev1.MatchTicket, error)

type matchWatch func(user uuid.UUID) (<-chan struct{}, func())

type matchFeed struct {
	listeners map[chan struct{}]struct{}
	ticket    *gamev1.MatchTicket
	err       error
	ready     bool
	stop      context.CancelFunc
}

type matchFeeds struct {
	mu       sync.Mutex
	limit    int
	interval time.Duration
	poll     matchPoll
	watch    matchWatch
	sockets  map[uuid.UUID]int
	feeds    map[matchKey]*matchFeed
	running  atomic.Int64
}

type matchListener struct {
	signal <-chan struct{}
	latest func() (*gamev1.MatchTicket, error)
	leave  func()
}

func newMatchFeeds(limit int, interval time.Duration, poll matchPoll, watch matchWatch) *matchFeeds {
	if limit <= 0 {
		limit = DefaultMatchSockets
	}
	return &matchFeeds{limit: limit, interval: interval, poll: poll, watch: watch, sockets: map[uuid.UUID]int{}, feeds: map[matchKey]*matchFeed{}}
}

func (m *matchFeeds) join(key matchKey) (*matchListener, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sockets[key.user] >= m.limit {
		return nil, false
	}
	m.sockets[key.user]++
	feed := m.feeds[key]
	if feed == nil {
		ctx, stop := context.WithCancel(context.Background())
		feed = &matchFeed{listeners: map[chan struct{}]struct{}{}, stop: stop}
		m.feeds[key] = feed
		m.running.Add(1)
		go m.run(ctx, key, feed)
	}
	signal := make(chan struct{}, 1)
	feed.listeners[signal] = struct{}{}
	if feed.ready {
		signal <- struct{}{}
	}
	var once sync.Once
	return &matchListener{
		signal: signal,
		latest: func() (*gamev1.MatchTicket, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			return feed.ticket, feed.err
		},
		leave: func() { once.Do(func() { m.leave(key, feed, signal) }) },
	}, true
}

func (m *matchFeeds) leave(key matchKey, feed *matchFeed, signal chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(feed.listeners, signal)
	if m.sockets[key.user]--; m.sockets[key.user] <= 0 {
		delete(m.sockets, key.user)
	}
	if len(feed.listeners) > 0 {
		return
	}
	if m.feeds[key] == feed {
		delete(m.feeds, key)
	}
	feed.stop()
}

func (m *matchFeeds) publish(key matchKey, feed *matchFeed, ticket *gamev1.MatchTicket, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	feed.ticket, feed.err, feed.ready = ticket, err, true
	if err != nil && m.feeds[key] == feed {
		delete(m.feeds, key)
	}
	for signal := range feed.listeners {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
}

func (m *matchFeeds) run(ctx context.Context, key matchKey, feed *matchFeed) {
	defer m.running.Add(-1)
	changed, unsubscribe := m.watch(key.user)
	defer unsubscribe()
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	touch := true
	for {
		ticket, err := m.poll(ctx, key, touch)
		if ctx.Err() != nil {
			return
		}
		m.publish(key, feed, ticket, err)
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-changed:
			touch = false
		case <-ticker.C:
			touch = true
		}
	}
}

func (m *matchFeeds) active() int64 { return m.running.Load() }

func (h *Handler) pollMatch(ctx context.Context, key matchKey, touch bool) (*gamev1.MatchTicket, error) {
	if touch {
		if err := h.store.Touch(ctx, key.user, key.game); err != nil {
			return nil, err
		}
	}
	found, err := h.store.Ticket(ctx, key.user, key.game)
	return ticketView(string(key.game), found, err)
}

func (h *Handler) watchMatches(user uuid.UUID) (<-chan struct{}, func()) {
	changed, _, unsubscribe := h.matches.subscribe(user)
	return changed, unsubscribe
}

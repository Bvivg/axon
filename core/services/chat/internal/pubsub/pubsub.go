// Package pubsub carries a message from the instance that wrote it to every
// instance holding somebody who should see it.
//
// It is delivery, never storage. Everything that goes through here is already
// committed to Postgres, and a client that misses something asks for it again
// by position — so losing a message in transit costs a round trip, not the
// message. That is what makes it safe for the implementations here to drop
// rather than block when a subscriber falls behind: the alternative is one slow
// reader stalling the sender that is waiting to be acknowledged.
package pubsub

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// Bus fans messages out to whoever is listening to a room.
//
// The instance that publishes also receives its own message back through its
// own subscription. That is deliberate: delivery then has exactly one path
// through the code, rather than a local one and a remote one that can drift
// apart in ordering or in shape.
type Bus interface {
	// Publish sends a message to every subscriber of its room, on this
	// instance and on every other.
	Publish(ctx context.Context, m domain.Message) error

	// Subscribe returns a channel of the room's messages and a function that
	// stops the subscription. The channel is closed once that function returns
	// or the context is done.
	Subscribe(ctx context.Context, roomID uuid.UUID) (<-chan domain.Message, func(), error)
}

// Backlog is how many messages a subscriber may fall behind by before its
// deliveries start being dropped.
//
// Small on purpose. A reader this far behind is not going to catch up by being
// given more room; it is going to reconnect and ask for what it missed, which
// is cheaper for everyone than memory held on its behalf.
const Backlog = 64

// Memory is a Bus confined to one process.
//
// It is the whole implementation for a single-instance deployment, and it is
// what the tests use. Running more than one instance needs the Redis one, which
// wraps this rather than replacing it: local delivery is the same problem
// wherever the message came from.
type Memory struct {
	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*subscription]struct{}
}

type subscription struct {
	ch chan domain.Message
}

// NewMemory returns an empty in-process bus.
func NewMemory() *Memory {
	return &Memory{rooms: make(map[uuid.UUID]map[*subscription]struct{})}
}

// Publish delivers to this process's subscribers.
func (m *Memory) Publish(_ context.Context, msg domain.Message) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for sub := range m.rooms[msg.RoomID] {
		select {
		case sub.ch <- msg:
		default:
			// Dropped, see the package comment: the subscriber will notice the
			// gap in positions and catch up from Postgres.
		}
	}

	return nil
}

// Subscribe registers a listener for one room.
func (m *Memory) Subscribe(ctx context.Context, roomID uuid.UUID) (<-chan domain.Message, func(), error) {
	sub := &subscription{ch: make(chan domain.Message, Backlog)}

	m.mu.Lock()
	if m.rooms[roomID] == nil {
		m.rooms[roomID] = make(map[*subscription]struct{})
	}
	m.rooms[roomID][sub] = struct{}{}
	m.mu.Unlock()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.rooms[roomID], sub)
			if len(m.rooms[roomID]) == 0 {
				delete(m.rooms, roomID)
			}
			m.mu.Unlock()

			// Closed under no lock but after removal, so Publish can no longer
			// reach it: sending on a closed channel would panic, and a bus that
			// panics takes every conversation on the instance with it.
			close(sub.ch)
		})
	}

	// A caller that forgets to stop still gets cleaned up when its context
	// ends, which for a socket is when the socket does.
	go func() {
		<-ctx.Done()
		stop()
	}()

	return sub.ch, stop, nil
}

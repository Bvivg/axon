package pubsub

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type Bus interface {
	Publish(ctx context.Context, m domain.Message) error

	Subscribe(ctx context.Context, roomID uuid.UUID) (<-chan domain.Message, func(), error)
}

const Backlog = 64

type Memory struct {
	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*subscription]struct{}
}

type subscription struct {
	ch chan domain.Message
}

func NewMemory() *Memory {
	return &Memory{rooms: make(map[uuid.UUID]map[*subscription]struct{})}
}

func (m *Memory) Publish(_ context.Context, msg domain.Message) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for sub := range m.rooms[msg.RoomID] {
		select {
		case sub.ch <- msg:
		default:

		}
	}

	return nil
}

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

			close(sub.ch)
		})
	}

	go func() {
		<-ctx.Done()
		stop()
	}()

	return sub.ch, stop, nil
}

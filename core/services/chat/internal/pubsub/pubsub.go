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

type SignalKind string

const (
	SignalRead SignalKind = "read"

	SignalTyping SignalKind = "typing"

	SignalRoomAdded SignalKind = "room_added"
)

type Signal struct {
	Kind   SignalKind
	RoomID uuid.UUID
	UserID uuid.UUID
	Seq    int64
}

type SignalBus interface {
	PublishSignal(ctx context.Context, s Signal) error

	SubscribeSignals(ctx context.Context, roomID uuid.UUID) (<-chan Signal, func(), error)
}

type UserSignalBus interface {
	PublishUserSignal(ctx context.Context, userID uuid.UUID, s Signal) error

	SubscribeUserSignals(ctx context.Context, userID uuid.UUID) (<-chan Signal, func(), error)
}

const Backlog = 64

type Memory struct {
	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*subscription]struct{}

	signals     *signalHub
	userSignals *signalHub
}

type subscription struct {
	ch chan domain.Message
}

type signalSubscription struct {
	ch chan Signal
}

func NewMemory() *Memory {
	return &Memory{
		rooms:       make(map[uuid.UUID]map[*subscription]struct{}),
		signals:     newSignalHub(),
		userSignals: newSignalHub(),
	}
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

func (m *Memory) PublishSignal(_ context.Context, sig Signal) error {
	m.signals.publish(sig.RoomID, sig)
	return nil
}

func (m *Memory) SubscribeSignals(ctx context.Context, roomID uuid.UUID) (<-chan Signal, func(), error) {
	ch, stop := m.signals.subscribe(ctx, roomID)
	return ch, stop, nil
}

func (m *Memory) PublishUserSignal(_ context.Context, userID uuid.UUID, sig Signal) error {
	m.userSignals.publish(userID, sig)
	return nil
}

func (m *Memory) SubscribeUserSignals(ctx context.Context, userID uuid.UUID) (<-chan Signal, func(), error) {
	ch, stop := m.userSignals.subscribe(ctx, userID)
	return ch, stop, nil
}

type signalHub struct {
	mu     sync.RWMutex
	topics map[uuid.UUID]map[*signalSubscription]struct{}
}

func newSignalHub() *signalHub {
	return &signalHub{topics: make(map[uuid.UUID]map[*signalSubscription]struct{})}
}

func (h *signalHub) publish(topic uuid.UUID, sig Signal) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for sub := range h.topics[topic] {
		select {
		case sub.ch <- sig:
		default:
		}
	}
}

func (h *signalHub) subscribe(ctx context.Context, topic uuid.UUID) (<-chan Signal, func()) {
	sub := &signalSubscription{ch: make(chan Signal, Backlog)}

	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = make(map[*signalSubscription]struct{})
	}
	h.topics[topic][sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.topics[topic], sub)
			if len(h.topics[topic]) == 0 {
				delete(h.topics, topic)
			}
			h.mu.Unlock()

			close(sub.ch)
		})
	}

	go func() {
		<-ctx.Done()
		stop()
	}()

	return sub.ch, stop
}

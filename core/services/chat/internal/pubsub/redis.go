package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type Redis struct {
	client goredis.UniversalClient
	local  *Memory
	log    *slog.Logger

	mu    sync.Mutex
	rooms map[uuid.UUID]*roomSubscription

	roomSignals *signalTopic
	userSignals *signalTopic
}

type roomSubscription struct {
	sub *goredis.PubSub

	listeners int

	stop context.CancelFunc
	done chan struct{}
}

func NewRedis(client goredis.UniversalClient, log *slog.Logger) (*Redis, error) {
	if client == nil {
		return nil, errors.New("pubsub: a redis client is required")
	}
	if log == nil {
		return nil, errors.New("pubsub: a logger is required")
	}

	local := NewMemory()

	return &Redis{
		client: client,
		local:  local,
		log:    log,
		rooms:  make(map[uuid.UUID]*roomSubscription),
		roomSignals: &signalTopic{
			name:    "room",
			channel: signalChannel,
			deliver: func(_ uuid.UUID, sig Signal) { local.signals.publish(sig.RoomID, sig) },
			subs:    make(map[uuid.UUID]*roomSubscription),
		},
		userSignals: &signalTopic{
			name:    "user",
			channel: userSignalChannel,
			deliver: func(userID uuid.UUID, sig Signal) { local.userSignals.publish(userID, sig) },
			subs:    make(map[uuid.UUID]*roomSubscription),
		},
	}, nil
}

func channel(roomID uuid.UUID) string { return "chat:room:" + roomID.String() }

func signalChannel(roomID uuid.UUID) string { return "chat:room:" + roomID.String() + ":signals" }

func userSignalChannel(userID uuid.UUID) string { return "chat:user:" + userID.String() + ":signals" }

type signalWire struct {
	Kind   string `json:"kind"`
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
	Seq    int64  `json:"seq,omitempty"`
}

func toSignalWire(sig Signal) signalWire {
	return signalWire{Kind: string(sig.Kind), RoomID: sig.RoomID.String(), UserID: sig.UserID.String(), Seq: sig.Seq}
}

func (w signalWire) toDomain() (Signal, error) {
	roomID, err := uuid.Parse(w.RoomID)
	if err != nil {
		return Signal{}, fmt.Errorf("pubsub: signal room id: %w", err)
	}
	userID, err := uuid.Parse(w.UserID)
	if err != nil {
		return Signal{}, fmt.Errorf("pubsub: signal user id: %w", err)
	}

	return Signal{Kind: SignalKind(w.Kind), RoomID: roomID, UserID: userID, Seq: w.Seq}, nil
}

type wire struct {
	ID       string    `json:"id"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	Body     string    `json:"body"`
	Seq      int64     `json:"seq"`
	SentAt   time.Time `json:"sent_at"`
	ClientID string    `json:"client_id,omitempty"`
}

func toWire(m domain.Message) wire {
	return wire{
		ID:       m.ID.String(),
		RoomID:   m.RoomID.String(),
		AuthorID: m.AuthorID.String(),
		Body:     m.Body,
		Seq:      m.Seq,
		SentAt:   m.SentAt,
		ClientID: m.ClientID,
	}
}

func (w wire) toDomain() (domain.Message, error) {
	id, err := uuid.Parse(w.ID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: message id: %w", err)
	}
	roomID, err := uuid.Parse(w.RoomID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: room id: %w", err)
	}
	authorID, err := uuid.Parse(w.AuthorID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: author id: %w", err)
	}

	return domain.Message{
		ID:       id,
		RoomID:   roomID,
		AuthorID: authorID,
		Body:     w.Body,
		Seq:      w.Seq,
		SentAt:   w.SentAt,
		ClientID: w.ClientID,
	}, nil
}

func (r *Redis) Publish(ctx context.Context, m domain.Message) error {
	payload, err := json.Marshal(toWire(m))
	if err != nil {
		return fmt.Errorf("pubsub: encode message: %w", err)
	}

	if err := r.client.Publish(ctx, channel(m.RoomID), payload).Err(); err != nil {
		return fmt.Errorf("pubsub: publish to %s: %w", channel(m.RoomID), err)
	}
	return nil
}

func (r *Redis) Subscribe(ctx context.Context, roomID uuid.UUID) (<-chan domain.Message, func(), error) {
	if err := r.acquire(roomID); err != nil {
		return nil, nil, err
	}

	messages, stopLocal, err := r.local.Subscribe(ctx, roomID)
	if err != nil {
		r.release(roomID)
		return nil, nil, err
	}

	var once sync.Once
	stop := func() {
		once.Do(func() {
			stopLocal()
			r.release(roomID)
		})
	}

	return messages, stop, nil
}

func (r *Redis) acquire(roomID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if room, ok := r.rooms[roomID]; ok {
		room.listeners++
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	sub := r.client.Subscribe(ctx, channel(roomID))
	room := &roomSubscription{sub: sub, listeners: 1, stop: cancel, done: make(chan struct{})}
	r.rooms[roomID] = room

	go r.pump(ctx, roomID, sub, room.done)

	return nil
}

func (r *Redis) release(roomID uuid.UUID) {
	r.mu.Lock()

	room, ok := r.rooms[roomID]
	if !ok {
		r.mu.Unlock()
		return
	}

	room.listeners--
	if room.listeners > 0 {
		r.mu.Unlock()
		return
	}

	delete(r.rooms, roomID)
	r.mu.Unlock()

	room.stop()
	if err := room.sub.Close(); err != nil {
		r.log.Debug("could not close a room subscription", "room_id", roomID, "error", err)
	}
	<-room.done
}

func (r *Redis) pump(ctx context.Context, roomID uuid.UUID, sub *goredis.PubSub, done chan<- struct{}) {
	defer close(done)

	messages := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}

			var w wire
			if err := json.Unmarshal([]byte(msg.Payload), &w); err != nil {
				r.log.Error("could not decode a fanned-out message",
					"room_id", roomID, "error", err)
				continue
			}

			m, err := w.toDomain()
			if err != nil {
				r.log.Error("a fanned-out message was malformed",
					"room_id", roomID, "error", err)
				continue
			}

			_ = r.local.Publish(ctx, m)
		}
	}
}

func (r *Redis) PublishSignal(ctx context.Context, sig Signal) error {
	return r.publishSignal(ctx, r.roomSignals, sig.RoomID, sig)
}

func (r *Redis) SubscribeSignals(ctx context.Context, roomID uuid.UUID) (<-chan Signal, func(), error) {
	return r.subscribeSignals(ctx, r.roomSignals, roomID, r.local.SubscribeSignals)
}

func (r *Redis) PublishUserSignal(ctx context.Context, userID uuid.UUID, sig Signal) error {
	return r.publishSignal(ctx, r.userSignals, userID, sig)
}

func (r *Redis) SubscribeUserSignals(ctx context.Context, userID uuid.UUID) (<-chan Signal, func(), error) {
	return r.subscribeSignals(ctx, r.userSignals, userID, r.local.SubscribeUserSignals)
}

type signalTopic struct {
	name    string
	channel func(uuid.UUID) string
	deliver func(uuid.UUID, Signal)
	subs    map[uuid.UUID]*roomSubscription
}

func (r *Redis) publishSignal(ctx context.Context, topic *signalTopic, id uuid.UUID, sig Signal) error {
	payload, err := json.Marshal(toSignalWire(sig))
	if err != nil {
		return fmt.Errorf("pubsub: encode %s signal: %w", sig.Kind, err)
	}

	name := topic.channel(id)
	if err := r.client.Publish(ctx, name, payload).Err(); err != nil {
		return fmt.Errorf("pubsub: publish to %s: %w", name, err)
	}
	return nil
}

func (r *Redis) subscribeSignals(
	ctx context.Context,
	topic *signalTopic,
	id uuid.UUID,
	local func(context.Context, uuid.UUID) (<-chan Signal, func(), error),
) (<-chan Signal, func(), error) {
	r.acquireSignals(topic, id)

	signals, stopLocal, err := local(ctx, id)
	if err != nil {
		r.releaseSignals(topic, id)
		return nil, nil, err
	}

	var once sync.Once
	stop := func() {
		once.Do(func() {
			stopLocal()
			r.releaseSignals(topic, id)
		})
	}

	return signals, stop, nil
}

func (r *Redis) acquireSignals(topic *signalTopic, id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := topic.subs[id]; ok {
		existing.listeners++
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	sub := r.client.Subscribe(ctx, topic.channel(id))
	subscription := &roomSubscription{sub: sub, listeners: 1, stop: cancel, done: make(chan struct{})}
	topic.subs[id] = subscription

	go r.pumpSignals(ctx, topic, id, sub, subscription.done)
}

func (r *Redis) releaseSignals(topic *signalTopic, id uuid.UUID) {
	r.mu.Lock()

	subscription, ok := topic.subs[id]
	if !ok {
		r.mu.Unlock()
		return
	}

	subscription.listeners--
	if subscription.listeners > 0 {
		r.mu.Unlock()
		return
	}

	delete(topic.subs, id)
	r.mu.Unlock()

	subscription.stop()
	if err := subscription.sub.Close(); err != nil {
		r.log.Debug("could not close a signal subscription", "topic", topic.name, "id", id, "error", err)
	}
	<-subscription.done
}

func (r *Redis) pumpSignals(
	ctx context.Context,
	topic *signalTopic,
	id uuid.UUID,
	sub *goredis.PubSub,
	done chan<- struct{},
) {
	defer close(done)

	messages := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}

			var w signalWire
			if err := json.Unmarshal([]byte(msg.Payload), &w); err != nil {
				r.log.Error("could not decode a fanned-out signal", "topic", topic.name, "id", id, "error", err)
				continue
			}

			sig, err := w.toDomain()
			if err != nil {
				r.log.Error("a fanned-out signal was malformed", "topic", topic.name, "id", id, "error", err)
				continue
			}

			topic.deliver(id, sig)
		}
	}
}

func (r *Redis) Close() error {
	r.mu.Lock()
	rooms := r.rooms
	r.rooms = make(map[uuid.UUID]*roomSubscription)
	signalSubs := make(map[uuid.UUID]*roomSubscription)
	for _, topic := range []*signalTopic{r.roomSignals, r.userSignals} {
		for id, sub := range topic.subs {
			signalSubs[id] = sub
		}
		topic.subs = make(map[uuid.UUID]*roomSubscription)
	}
	r.mu.Unlock()

	for id, room := range rooms {
		room.stop()
		if err := room.sub.Close(); err != nil {
			r.log.Debug("could not close a room subscription", "room_id", id, "error", err)
		}
		<-room.done
	}
	for id, room := range signalSubs {
		room.stop()
		if err := room.sub.Close(); err != nil {
			r.log.Debug("could not close a signal subscription", "room_id", id, "error", err)
		}
		<-room.done
	}
	return nil
}

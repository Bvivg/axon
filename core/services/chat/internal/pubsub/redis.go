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

// Redis fans messages out across instances.
//
// It wraps Memory rather than replacing it, and everything arrives the same
// way: a message published here goes to Redis, comes back on the subscription
// this instance holds — including to the instance that sent it — and is handed
// to Memory, which delivers it to the sockets. One path, so the local case
// cannot drift from the remote one in ordering or in shape.
//
// Nothing here is storage. Redis losing everything costs delivery in the moment
// and no messages: they are in Postgres, and a client catches up by position.
// That is what rules/infra.md means by Redis holding nothing that is not
// reconstructible.
type Redis struct {
	client goredis.UniversalClient
	local  *Memory
	log    *slog.Logger

	// mu guards rooms. One Redis subscription per room, however many sockets on
	// this instance are listening to it — subscribing per socket would multiply
	// connections by conversations.
	mu    sync.Mutex
	rooms map[uuid.UUID]*roomSubscription
}

type roomSubscription struct {
	sub *goredis.PubSub

	// listeners is how many local sockets want this room. The Redis
	// subscription is dropped when it reaches zero.
	listeners int

	stop context.CancelFunc
	done chan struct{}
}

// NewRedis returns a Bus backed by Redis pub/sub.
func NewRedis(client goredis.UniversalClient, log *slog.Logger) (*Redis, error) {
	if client == nil {
		return nil, errors.New("pubsub: a redis client is required")
	}
	if log == nil {
		return nil, errors.New("pubsub: a logger is required")
	}

	return &Redis{
		client: client,
		local:  NewMemory(),
		log:    log,
		rooms:  make(map[uuid.UUID]*roomSubscription),
	}, nil
}

// channel is where a room's messages travel.
func channel(roomID uuid.UUID) string { return "chat:room:" + roomID.String() }

// wire is a message as it crosses Redis.
//
// Its own type rather than domain.Message, for the same reason a Kafka payload
// has one: this is a format two processes agree on, and a field renamed in the
// domain must not silently change what a running instance sends to one that has
// not been restarted yet.
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

// Publish sends a message to every instance holding somebody in the room.
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

// Subscribe registers a listener, opening the room's Redis subscription if this
// is the first one on this instance.
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

// acquire opens or reuses the room's Redis subscription.
func (r *Redis) acquire(roomID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if room, ok := r.rooms[roomID]; ok {
		room.listeners++
		return nil
	}

	// Background rather than a subscriber's context: this subscription outlives
	// whichever socket happened to open it, and dies when the last one goes.
	ctx, cancel := context.WithCancel(context.Background())

	sub := r.client.Subscribe(ctx, channel(roomID))
	room := &roomSubscription{sub: sub, listeners: 1, stop: cancel, done: make(chan struct{})}
	r.rooms[roomID] = room

	go r.pump(ctx, roomID, sub, room.done)

	return nil
}

// release drops one listener and closes the subscription when none are left.
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

// pump reads the room's channel and hands each message to the local bus.
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

			// Local delivery cannot fail and cannot block: Memory drops for a
			// subscriber that has fallen behind, which is the right answer here
			// too — one slow socket must not stall the room for everyone else.
			_ = r.local.Publish(ctx, m)
		}
	}
}

// Close drops every subscription this instance holds.
func (r *Redis) Close() error {
	r.mu.Lock()
	rooms := r.rooms
	r.rooms = make(map[uuid.UUID]*roomSubscription)
	r.mu.Unlock()

	for id, room := range rooms {
		room.stop()
		if err := room.sub.Close(); err != nil {
			r.log.Debug("could not close a room subscription", "room_id", id, "error", err)
		}
		<-room.done
	}
	return nil
}

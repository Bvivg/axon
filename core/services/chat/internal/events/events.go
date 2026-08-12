// Package events announces what happened in chat to the rest of the system.
//
// It is not how a message reaches the person it was sent to — that is the
// socket, fanned out through Redis. This is the durable record other domains
// subscribe to: notifications, analytics, anything that wants to know a
// conversation happened without being part of it.
//
// Nothing consumes chat.message yet. The topic exists now anyway, because the
// event contract is part of the service's design rather than a consequence of
// somebody needing it: adding it later would mean adding it to a service that
// has already been running without it, and backfilling what was never
// published.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// Topic names. <domain>.<event>, as rules/infra.md requires; kafka.Topic
// refuses anything else, so a typo fails at startup rather than creating a
// stray topic on a broker with auto-creation on.
var messageTopic = mustTopic("chat", "message")

// mustTopic builds a topic name that is a constant in everything but syntax.
// A failure here is a typo in this file, so it stops the process rather than
// travelling to a call site as an error nobody can act on.
func mustTopic(domain, event string) kafka.Topic {
	topic, err := kafka.NewTopic(domain, event)
	if err != nil {
		panic(fmt.Sprintf("events: %v", err))
	}
	return topic
}

// sink is the part of the producer this package uses. An interface so the
// payload can be asserted in a test without a broker: what is worth checking
// here is the shape of what goes out, and the transport underneath has its own
// tests against a real one.
type sink interface {
	Publish(ctx context.Context, msg kafka.Message) error
}

// Publisher announces chat events.
type Publisher struct {
	sink sink
	log  *slog.Logger
}

// New returns a Publisher over the given producer.
func New(producer sink, log *slog.Logger) *Publisher {
	return &Publisher{sink: producer, log: log}
}

// MessageSent is the payload of chat.message.
//
// It carries the body, not just a reference to it. A consumer that has to come
// back and ask what was said would need a way in to chat's database, which is
// exactly what schema isolation exists to prevent.
type MessageSent struct {
	MessageID string    `json:"message_id"`
	RoomID    string    `json:"room_id"`
	AuthorID  string    `json:"author_id"`
	Body      string    `json:"body"`
	Seq       int64     `json:"seq"`
	SentAt    time.Time `json:"sent_at"`
}

// MessageSent publishes one message onto the bus.
//
// It never returns an error, and the reason is the ordering it sits in: the
// message is already committed to Postgres and already on its way to everyone
// in the room. Failing the caller now would report a delivery that succeeded as
// a failure, and a client that retried would be resending something that was
// never lost.
//
// The cost is that the bus is at-most-once: a broker outage drops the event
// while the message itself survives. The fix for that is a transactional
// outbox — write the event in the same transaction as the message and let a
// relay drain it — and it is worth doing when something actually consumes this
// topic. Until then the honest description is the one in this comment rather
// than a retry loop that pretends otherwise.
func (p *Publisher) MessageSent(ctx context.Context, m domain.Message) {
	payload, err := json.Marshal(MessageSent{
		MessageID: m.ID.String(),
		RoomID:    m.RoomID.String(),
		AuthorID:  m.AuthorID.String(),
		Body:      m.Body,
		Seq:       m.Seq,
		SentAt:    m.SentAt,
	})
	if err != nil {
		p.log.ErrorContext(ctx, "could not encode a chat.message event",
			"message_id", m.ID, "error", err)
		return
	}

	// The room id is the partition key, and that is what makes a room's
	// messages arrive at a consumer in the order they were said. Ordering
	// matters inside a room and nowhere else, so keying any wider would
	// serialise unrelated conversations for nothing.
	err = p.sink.Publish(ctx, kafka.Message{
		Topic: messageTopic,
		Key:   []byte(m.RoomID.String()),
		Value: payload,
	})
	if err != nil {
		p.log.ErrorContext(ctx, "could not publish a chat.message event",
			"message_id", m.ID, "room_id", m.RoomID, "error", err)
	}
}

// Discard is a Publisher that announces nothing.
//
// A deployment with no broker configured is a working deployment: chat still
// delivers messages, and nothing consumes the topic anyway. This is what says
// so, instead of a nil check at every call site.
func Discard() *Publisher {
	return &Publisher{sink: discardSink{}, log: logger.Discard()}
}

type discardSink struct{}

func (discardSink) Publish(context.Context, kafka.Message) error { return nil }

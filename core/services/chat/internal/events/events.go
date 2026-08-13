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

var messageTopic = mustTopic("chat", "message")

func mustTopic(domain, event string) kafka.Topic {
	topic, err := kafka.NewTopic(domain, event)
	if err != nil {
		panic(fmt.Sprintf("events: %v", err))
	}
	return topic
}

type sink interface {
	Publish(ctx context.Context, msg kafka.Message) error
}

type Publisher struct {
	sink sink
	log  *slog.Logger
}

func New(producer sink, log *slog.Logger) *Publisher {
	return &Publisher{sink: producer, log: log}
}

type MessageSent struct {
	MessageID string    `json:"message_id"`
	RoomID    string    `json:"room_id"`
	AuthorID  string    `json:"author_id"`
	Body      string    `json:"body"`
	Seq       int64     `json:"seq"`
	SentAt    time.Time `json:"sent_at"`
}

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

func Discard() *Publisher {
	return &Publisher{sink: discardSink{}, log: logger.Discard()}
}

type discardSink struct{}

func (discardSink) Publish(context.Context, kafka.Message) error { return nil }

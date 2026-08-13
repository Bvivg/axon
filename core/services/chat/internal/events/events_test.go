package events

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/kafka"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type recordingSink struct {
	published []kafka.Message
	failWith  error
}

func (s *recordingSink) Publish(_ context.Context, msg kafka.Message) error {
	if s.failWith != nil {
		return s.failWith
	}
	s.published = append(s.published, msg)
	return nil
}

func newMessage() domain.Message {
	return domain.Message{
		ID:       uuid.New(),
		RoomID:   uuid.New(),
		AuthorID: uuid.New(),
		Body:     "hello",
		Seq:      7,
		SentAt:   time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

func TestMessageSentCarriesTheWholeMessage(t *testing.T) {
	sink := &recordingSink{}
	m := newMessage()

	New(sink, logger.Discard()).MessageSent(t.Context(), m)

	if len(sink.published) != 1 {
		t.Fatalf("published %d messages, want 1", len(sink.published))
	}
	published := sink.published[0]

	if published.Topic.String() != "chat.message" {
		t.Errorf("topic = %q, want chat.message", published.Topic)
	}

	if got := string(published.Key); got != m.RoomID.String() {
		t.Errorf("key = %q, want the room id %q", got, m.RoomID)
	}

	var payload MessageSent
	if err := json.Unmarshal(published.Value, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	want := MessageSent{
		MessageID: m.ID.String(),
		RoomID:    m.RoomID.String(),
		AuthorID:  m.AuthorID.String(),
		Body:      m.Body,
		Seq:       m.Seq,
		SentAt:    m.SentAt,
	}
	if payload != want {
		t.Errorf("payload = %+v, want %+v", payload, want)
	}

	if payload.Body == "" {
		t.Error("the event carries no body")
	}
}

func TestAFailedPublishIsSwallowed(t *testing.T) {
	sink := &recordingSink{failWith: errors.New("broker unreachable")}

	New(sink, logger.Discard()).MessageSent(t.Context(), newMessage())
}

func TestDiscardPublishesNowhere(t *testing.T) {
	Discard().MessageSent(t.Context(), newMessage())
}

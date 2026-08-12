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

// recordingSink stands in for the producer. The transport has its own tests
// against a real broker in shared/integration; what is worth checking here is
// the shape of what this package hands it.
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

	// The room id, so that one room's messages share a partition and therefore
	// arrive in order. Anything wider would serialise unrelated rooms; anything
	// narrower would let a room's own messages overtake each other.
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

	// The body travels with the event. A consumer that had to come back and ask
	// what was said would need a way into chat's database, which is the thing
	// schema isolation exists to prevent.
	if payload.Body == "" {
		t.Error("the event carries no body")
	}
}

// The message is already committed and already on its way to the room by the
// time this runs. A broker that will not take it is a lost event, not a failed
// send, and the caller has nothing to do about it.
func TestAFailedPublishIsSwallowed(t *testing.T) {
	sink := &recordingSink{failWith: errors.New("broker unreachable")}

	// The assertion is that this returns at all: MessageSent has no error to
	// return, so a panic or a block is the only way it could fail here.
	New(sink, logger.Discard()).MessageSent(t.Context(), newMessage())
}

func TestDiscardPublishesNowhere(t *testing.T) {
	Discard().MessageSent(t.Context(), newMessage())
}

package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const (
	OutboxTable      = "chat.outbox"
	MessageSentTopic = "chat.message.v1"
	CallEndedTopic   = "call.ended.v1"
)

func MessageSentEvent(id uuid.UUID, m domain.Message) outbox.Event {
	return outbox.Event{
		ID:          id,
		Topic:       MessageSentTopic,
		AggregateID: m.RoomID.String(),
		Payload: &eventsv1.ChatMessageSent{
			EventId:   id.String(),
			MessageId: m.ID.String(),
			RoomId:    m.RoomID.String(),
			SenderId:  m.AuthorID.String(),
			Seq:       m.Seq,
			Kind:      string(m.Kind),
			SentAt:    timestamppb.New(m.SentAt),
		},
	}
}

func CallEndedEvent(id uuid.UUID, c calls.Call) outbox.Event {
	participants := make([]string, 0, len(c.Participants))
	for _, p := range c.Participants {
		participants = append(participants, p.UserID.String())
	}
	var room string
	if c.RoomID != uuid.Nil {
		room = c.RoomID.String()
	}
	return outbox.Event{
		ID:          id,
		Topic:       CallEndedTopic,
		AggregateID: c.ID.String(),
		Payload: &eventsv1.CallEnded{
			EventId:      id.String(),
			CallId:       c.ID.String(),
			RoomId:       room,
			InitiatorId:  c.CallerID.String(),
			Participants: participants,
			StartedAt:    optionalTimestamp(c.StartedAt),
			EndedAt:      optionalTimestamp(c.EndedAt),
			Reason:       c.EndReason,
		},
	}
}

func optionalTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

func (r *Repository) recordEvent(ctx context.Context, event outbox.Event) error {
	if err := outbox.Write(ctx, r.q, OutboxTable, event); err != nil {
		return fmt.Errorf("repository: record %s: %w", event.Topic, err)
	}
	return nil
}

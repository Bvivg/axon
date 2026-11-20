package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const (
	OutboxTable         = "auth.outbox"
	UserRegisteredTopic = "auth.user.registered.v1"
)

func UserRegisteredEvent(id uuid.UUID, u domain.User) outbox.Event {
	return outbox.Event{
		ID:          id,
		Topic:       UserRegisteredTopic,
		AggregateID: u.ID.String(),
		Payload: &eventsv1.UserRegistered{
			EventId:      id.String(),
			UserId:       u.ID.String(),
			Username:     u.Username,
			RegisteredAt: timestamppb.New(u.CreatedAt),
		},
	}
}

func (r *Repository) recordRegistration(ctx context.Context, u domain.User) error {
	if err := outbox.Write(ctx, r.q, OutboxTable, UserRegisteredEvent(uuid.New(), u)); err != nil {
		return fmt.Errorf("repository: record registration: %w", err)
	}
	return nil
}

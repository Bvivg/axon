package calls

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type GroupStore interface {
	SetEndpointSFU(context.Context, Actor, EndpointRef, bool, time.Time) error
	ReplaceCall(context.Context, Actor, Command, time.Time) (Call, error)
}

type FamilyExpiryStore interface {
	ExpireCallFamily(context.Context, uuid.UUID, uuid.UUID, time.Time) error
}

func (s *Service) FamilyRevoked(ctx context.Context, user, family uuid.UUID) error {
	if store, ok := s.store.(FamilyExpiryStore); ok {
		return store.ExpireCallFamily(ctx, user, family, s.now().UTC())
	}
	return nil
}

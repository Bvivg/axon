package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func (s *Service) SubscribePush(ctx context.Context, sub domain.PushSubscription) error {
	valid, err := domain.ValidatePushSubscription(sub)
	if err != nil {
		return err
	}
	if err := s.store.SavePushSubscription(ctx, valid); err != nil {
		return fmt.Errorf("service: subscribe push: %w", err)
	}
	return nil
}

func (s *Service) UnsubscribePush(ctx context.Context, userID uuid.UUID, endpoint string) error {
	if err := s.store.DeletePushSubscription(ctx, userID, endpoint); err != nil {
		return fmt.Errorf("service: unsubscribe push: %w", err)
	}
	return nil
}

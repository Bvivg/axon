package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (s *Service) ListSessions(ctx context.Context, userID, currentFamilyID uuid.UUID) ([]domain.Session, error) {
	sessions, err := s.store.Sessions(ctx, userID, s.now().UTC())
	if err != nil {
		return nil, err
	}

	for i := range sessions {
		sessions[i].Current = sessions[i].FamilyID == currentFamilyID
	}
	return sessions, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	revoked, err := s.store.RevokeFamilyForUser(ctx, userID, sessionID, s.now().UTC())
	if err != nil {
		return err
	}
	if revoked == 0 {
		return domain.ErrSessionNotFound
	}

	s.log.InfoContext(ctx, "session revoked",
		"user_id", userID, "family_id", sessionID, "tokens_revoked", revoked)
	return nil
}

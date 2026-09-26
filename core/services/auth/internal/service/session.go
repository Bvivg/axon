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

	if s.presence == nil || len(sessions) == 0 {
		return sessions, nil
	}

	ids := make([]string, len(sessions))
	for i, session := range sessions {
		ids[i] = session.FamilyID.String()
	}

	online, err := s.presence.Online(ctx, ids)
	if err != nil {
		s.log.WarnContext(ctx, "could not check session presence", "user_id", userID, "error", err)
		return sessions, nil
	}

	for i := range sessions {
		sessions[i].Online = online[sessions[i].FamilyID.String()]
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

	s.publishFamilyRevocation(ctx, sessionID)

	return nil
}

func (s *Service) publishFamilyRevocation(ctx context.Context, familyID uuid.UUID) {
	if s.revocation == nil {
		return
	}
	if err := s.revocation.PublishFamily(ctx, familyID); err != nil {
		s.log.WarnContext(ctx, "could not publish session revocation", "family_id", familyID, "error", err)
	}
}

func (s *Service) publishUserRevocation(ctx context.Context, userID uuid.UUID) {
	if s.revocation == nil {
		return
	}
	if err := s.revocation.PublishUser(ctx, userID); err != nil {
		s.log.WarnContext(ctx, "could not publish session revocation", "user_id", userID, "error", err)
	}
}

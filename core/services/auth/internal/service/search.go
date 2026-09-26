package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (s *Service) SearchUsers(ctx context.Context, callerID uuid.UUID, query string) ([]domain.PublicProfile, error) {
	normalized, err := domain.ValidateSearchQuery(query)
	if err != nil {
		return nil, err
	}

	users, err := s.store.SearchExact(ctx, normalized, callerID)
	if err != nil {
		return nil, err
	}

	return s.withOnlineStatus(ctx, users), nil
}

func (s *Service) UsersPublicProfiles(ctx context.Context, userIDs []uuid.UUID) ([]domain.PublicProfile, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	users, err := s.store.UsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	return s.withOnlineStatus(ctx, users), nil
}

func (s *Service) withOnlineStatus(ctx context.Context, users []domain.User) []domain.PublicProfile {
	profiles := make([]domain.PublicProfile, len(users))
	for i, u := range users {
		profiles[i] = domain.PublicProfile{User: u}
	}

	if s.presence == nil || len(users) == 0 {
		return profiles
	}

	ids := make([]uuid.UUID, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}

	families, err := s.store.ActiveFamiliesForUsers(ctx, ids, s.now().UTC())
	if err != nil {
		s.log.WarnContext(ctx, "could not load active sessions for presence", "error", err)
		return profiles
	}

	var familyIDs []string
	for _, userFamilies := range families {
		for _, familyID := range userFamilies {
			familyIDs = append(familyIDs, familyID.String())
		}
	}
	if len(familyIDs) == 0 {
		return profiles
	}

	online, err := s.presence.Online(ctx, familyIDs)
	if err != nil {
		s.log.WarnContext(ctx, "could not check presence", "error", err)
		return profiles
	}

	for i, u := range users {
		for _, familyID := range families[u.ID] {
			if online[familyID.String()] {
				profiles[i].Online = true
				break
			}
		}
	}

	return profiles
}

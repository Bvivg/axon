package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const searchResultLimit = 20

type searchPageToken struct {
	Query string `json:"q"`
	domain.UserSearchCursor
}

func (s *Service) SearchUsers(ctx context.Context, callerID uuid.UUID, query string, pageSize int32, pageToken string) ([]domain.PublicProfile, string, error) {
	normalized, err := domain.ValidateSearchQuery(query)
	if err != nil {
		return nil, "", err
	}
	if pageSize < 0 || pageSize > 100 {
		return nil, "", &domain.ValidationError{Field: "page_size", Reason: "must be between 1 and 100"}
	}
	limit := int(pageSize)
	if limit == 0 {
		limit = searchResultLimit
	}
	var after *domain.UserSearchCursor
	if pageToken != "" {
		var token searchPageToken
		data, decodeErr := base64.RawURLEncoding.DecodeString(pageToken)
		if len(pageToken) > 2048 || decodeErr != nil || json.Unmarshal(data, &token) != nil ||
			token.Query != strings.ToLower(normalized) || token.ID == uuid.Nil || token.Rank < 0 || token.Rank > 2 {
			return nil, "", &domain.ValidationError{Field: "page_token", Reason: "is invalid for this query"}
		}
		after = &token.UserSearchCursor
	}

	results, err := s.store.SearchUsers(ctx, normalized, callerID, limit+1, after)
	if err != nil {
		return nil, "", err
	}
	var next string
	if len(results) > limit {
		results = results[:limit]
		data, err := json.Marshal(searchPageToken{Query: strings.ToLower(normalized), UserSearchCursor: results[limit-1].Cursor})
		if err != nil {
			return nil, "", err
		}
		next = base64.RawURLEncoding.EncodeToString(data)
	}
	users := make([]domain.User, len(results))
	for i, result := range results {
		users[i] = result.User
	}

	return s.withOnlineStatus(ctx, users), next, nil
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

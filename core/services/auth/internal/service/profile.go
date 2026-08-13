package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, patch domain.ProfilePatch) (domain.User, error) {
	current, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}

	update := domain.ProfileUpdate{
		Email:         current.Email,
		EmailVerified: current.EmailVerified,
		FirstName:     current.FirstName,
		LastName:      current.LastName,
		Nickname:      current.Nickname,
	}

	if patch.Email != nil {
		email, err := domain.ValidateEmail(*patch.Email)
		if err != nil {
			return domain.User{}, err
		}

		if email != current.Email {
			update.Email = email
			update.EmailVerified = false
		}
	}

	if patch.FirstName != nil {
		v, err := domain.ValidateNameField("first_name", *patch.FirstName)
		if err != nil {
			return domain.User{}, err
		}
		update.FirstName = v
	}
	if patch.LastName != nil {
		v, err := domain.ValidateNameField("last_name", *patch.LastName)
		if err != nil {
			return domain.User{}, err
		}
		update.LastName = v
	}
	if patch.Nickname != nil {
		v, err := domain.ValidateNameField("nickname", *patch.Nickname)
		if err != nil {
			return domain.User{}, err
		}
		update.Nickname = v
	}

	update.DisplayName = resolveDisplayName(update.Nickname, update.FirstName, update.LastName, current.DisplayName)

	updated, err := s.store.UpdateProfile(ctx, userID, update)
	if err != nil {
		return domain.User{}, err
	}

	s.log.InfoContext(ctx, "profile updated", "user_id", userID)
	return updated, nil
}

func resolveDisplayName(nickname, firstName, lastName, previous string) string {
	if nickname != "" {
		return nickname
	}
	if full := strings.TrimSpace(firstName + " " + lastName); full != "" {
		return full
	}
	return previous
}

package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (s *Service) UpdateProfile(ctx context.Context, userID, sessionFamilyID uuid.UUID, patch domain.ProfilePatch) (domain.User, error) {
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
		Username:      current.Username,
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

	if patch.Username != nil && domain.NormalizeUsername(*patch.Username) != current.Username {
		v, err := domain.ValidateUsername(*patch.Username)
		if err != nil {
			return domain.User{}, err
		}
		update.Username = v
	}

	update.DisplayName = resolveDisplayName(update.Nickname, update.FirstName, update.LastName, current.DisplayName)

	if update.Email == current.Email {
		updated, err := s.store.UpdateProfile(ctx, userID, update)
		if err != nil {
			return domain.User{}, err
		}
		s.log.InfoContext(ctx, "profile updated", "user_id", userID)
		return s.attachRoles(ctx, updated), nil
	}

	updated, revoked, err := s.store.UpdateProfileRevokingOtherSessions(ctx, userID, update, sessionFamilyID, s.now().UTC())
	if err != nil {
		return domain.User{}, err
	}

	s.log.InfoContext(ctx, "email changed, other sessions revoked", "user_id", userID, "sessions_revoked", len(revoked))

	subject := ""
	if sessionFamilyID != uuid.Nil {
		subject = sessionFamilyID.String()
	}
	s.audit(ctx, domain.SecurityEvent{
		Action:      domain.SecurityEmailChanged,
		UserID:      userID,
		ActorID:     userID,
		Subject:     subject,
		EmailSHA256: domain.EmailSHA256(update.Email),
	})

	for _, familyID := range revoked {
		s.publishFamilyRevocation(ctx, familyID)
	}

	return s.attachRoles(ctx, updated), nil
}

func (s *Service) UpdateLastSeen(ctx context.Context, userID uuid.UUID) error {
	return s.store.UpdateLastSeen(ctx, userID, s.now())
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

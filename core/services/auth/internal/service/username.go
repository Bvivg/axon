package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const usernameAttempts = 5

func (s *Service) createWithUsername(
	ctx context.Context,
	chosen string,
	email string,
	create func(username string) (domain.User, error),
) (domain.User, error) {
	if chosen != "" {
		return create(chosen)
	}

	stem := domain.UsernameStem(email)
	for attempt := range usernameAttempts {
		candidate := stem
		if attempt > 0 || domain.IsReservedUsername(stem) {
			candidate = domain.UsernameWithSuffix(stem, usernameSuffix())
		}

		user, err := create(candidate)
		if !errors.Is(err, domain.ErrUsernameTaken) {
			return user, err
		}
	}
	return domain.User{}, fmt.Errorf("service: generate a username after %d attempts: %w",
		usernameAttempts, domain.ErrUsernameTaken)
}

func usernameSuffix() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
}

func (s *Service) PublicProfileByUsername(ctx context.Context, username string) (domain.PublicProfile, error) {
	name := domain.NormalizeUsername(username)
	if name == "" {
		return domain.PublicProfile{}, domain.ErrUserNotFound
	}

	user, err := s.store.UserByUsername(ctx, name)
	if err != nil {
		return domain.PublicProfile{}, err
	}

	return s.withOnlineStatus(ctx, []domain.User{user})[0], nil
}

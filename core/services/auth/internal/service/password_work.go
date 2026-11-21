package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/limit"
)

func (s *Service) hashPassword(ctx context.Context, plaintext string) (string, error) {
	release, err := s.acquireHashing(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return s.hasher.Hash(plaintext)
}

func (s *Service) verifyPassword(ctx context.Context, plaintext, encoded string) (bool, error) {
	release, err := s.acquireHashing(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	return s.hasher.Verify(plaintext, encoded)
}

func (s *Service) acquireHashing(ctx context.Context) (func(), error) {
	release, err := s.passwordGate.Acquire(ctx)
	if errors.Is(err, limit.ErrSaturated) {
		s.log.WarnContext(ctx, "password hashing is at capacity, request refused")
		return nil, domain.ErrPasswordHashingBusy
	}
	return release, err
}

func (s *Service) checkLoginThrottle(ctx context.Context, account string) error {
	if s.loginThrottle == nil {
		return nil
	}
	remaining, err := s.loginThrottle.Blocked(ctx, account)
	if err != nil {
		s.log.WarnContext(ctx, "could not read the sign-in throttle", "error", err)
		return nil
	}
	if remaining > 0 {
		s.log.InfoContext(ctx, "sign-in refused while the account is throttled", "retry_after", remaining)
		return &domain.ThrottledError{RetryAfter: remaining}
	}
	return nil
}

func (s *Service) noteLoginFailure(ctx context.Context, userID uuid.UUID, account string) {
	if s.loginThrottle == nil {
		return
	}
	lock, err := s.loginThrottle.Failed(ctx, account)
	if err != nil {
		s.log.WarnContext(ctx, "could not record a failed sign-in", "error", err)
		return
	}
	if lock > 0 {
		s.log.WarnContext(ctx, "account throttled after repeated failed sign-ins",
			"user_id", userID, "lock", lock.Round(time.Second))
	}
}

func (s *Service) noteLoginSuccess(ctx context.Context, account string) {
	if s.loginThrottle == nil {
		return
	}
	if err := s.loginThrottle.Succeeded(ctx, account); err != nil {
		s.log.WarnContext(ctx, "could not clear the sign-in throttle", "error", err)
	}
}

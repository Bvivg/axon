package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

type Counter interface {
	Inc()
}

type discardCounter struct{}

func (discardCounter) Inc() {}

const auditWriteTimeout = 5 * time.Second

func (s *Service) audit(ctx context.Context, e domain.SecurityEvent) {
	e.Device = domain.DeviceFromContext(ctx)
	e.OccurredAt = s.now().UTC()

	write, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditWriteTimeout)
	defer cancel()

	if err := s.store.RecordSecurityEvent(write, e); err != nil {
		s.auditFailures.Inc()
		s.log.ErrorContext(ctx, "could not record a security event",
			"action", string(e.Action),
			"user_id", e.UserID,
			"subject", e.Subject,
			"error", err,
		)
	}
}

func (s *Service) loginFailed(ctx context.Context, userID uuid.UUID, email string) error {
	s.audit(ctx, domain.SecurityEvent{
		Action:      domain.SecurityLoginFailed,
		UserID:      userID,
		EmailSHA256: domain.EmailSHA256(email),
	})
	return domain.ErrInvalidCredentials
}

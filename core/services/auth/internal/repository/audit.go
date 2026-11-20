package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const (
	SecurityAuditTopic = "auth.audit.v1"
	AnonymousAggregate = "anonymous"
)

func SecurityAuditEvent(id uuid.UUID, e domain.SecurityEvent) outbox.Event {
	aggregate := AnonymousAggregate
	if e.UserID != uuid.Nil {
		aggregate = e.UserID.String()
	}
	return outbox.Event{
		ID:          id,
		Topic:       SecurityAuditTopic,
		AggregateID: aggregate,
		Payload: &eventsv1.SecurityEvent{
			EventId:     id.String(),
			Action:      securityAction(e.Action),
			UserId:      idOrEmpty(e.UserID),
			ActorId:     idOrEmpty(e.ActorID),
			Subject:     e.Subject,
			ClientIp:    e.Device.IP,
			UserAgent:   e.Device.UserAgent,
			EmailSha256: e.EmailSHA256,
			OccurredAt:  timestamppb.New(e.OccurredAt),
		},
	}
}

func (r *Repository) RecordSecurityEvent(ctx context.Context, e domain.SecurityEvent) error {
	if err := outbox.Write(ctx, r.q, OutboxTable, SecurityAuditEvent(uuid.New(), e)); err != nil {
		return fmt.Errorf("repository: record security event %s: %w", e.Action, err)
	}
	return nil
}

func securityAction(a domain.SecurityAction) eventsv1.SecurityAction {
	switch a {
	case domain.SecurityLoginSucceeded:
		return eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED
	case domain.SecurityLoginFailed:
		return eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED
	case domain.SecurityLoggedOut:
		return eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT
	case domain.SecurityLoggedOutEverywhere:
		return eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT_EVERYWHERE
	case domain.SecuritySessionRevoked:
		return eventsv1.SecurityAction_SECURITY_ACTION_SESSION_REVOKED
	case domain.SecurityRefreshTokenReused:
		return eventsv1.SecurityAction_SECURITY_ACTION_REFRESH_TOKEN_REUSED
	case domain.SecurityRoleAssigned:
		return eventsv1.SecurityAction_SECURITY_ACTION_ROLE_ASSIGNED
	case domain.SecurityRoleRevoked:
		return eventsv1.SecurityAction_SECURITY_ACTION_ROLE_REVOKED
	case domain.SecurityEmailChanged:
		return eventsv1.SecurityAction_SECURITY_ACTION_EMAIL_CHANGED
	default:
		return eventsv1.SecurityAction_SECURITY_ACTION_UNSPECIFIED
	}
}

func idOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

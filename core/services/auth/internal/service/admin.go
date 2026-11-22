package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const (
	auditService   = "auth"
	auditUser      = "user"
	actionSuspend  = "user_suspended"
	actionRestore  = "user_restored"
	actionSessions = "user_sessions_revoked"
	actionGrant    = "role_assigned"
	actionRevoke   = "role_revoked"
	actionViewed   = "user_viewed"
)

func (s *Service) Permissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.store.Permissions(ctx, userID)
}

func (s *Service) ListUsers(ctx context.Context, f domain.UserFilter) (domain.UserPage, error) {
	return s.store.ListUsers(ctx, f, s.now().UTC())
}

func (s *Service) AdminUser(ctx context.Context, actorID, userID uuid.UUID) (domain.AdminUser, []domain.Session, error) {
	now := s.now().UTC()
	user, err := s.store.AdminUser(ctx, userID, now)
	if err != nil {
		return domain.AdminUser{}, nil, err
	}
	sessions, err := s.store.Sessions(ctx, userID, now)
	if err != nil {
		return domain.AdminUser{}, nil, err
	}
	if err := s.store.RecordAdminAction(ctx, s.adminAction(ctx, actorID, actionViewed, userID, "")); err != nil {
		return domain.AdminUser{}, nil, fmt.Errorf("auth: record a viewed account: %w", err)
	}
	return user, sessions, nil
}

func (s *Service) SuspendUser(ctx context.Context, actorID, userID uuid.UUID, reason string) (domain.AdminUser, error) {
	if actorID == userID {
		return domain.AdminUser{}, domain.ErrSelfAction
	}
	now := s.now().UTC()
	action := s.adminAction(ctx, actorID, actionSuspend, userID, reason)
	if _, err := s.store.SuspendUser(ctx, userID, reason, now, action); err != nil {
		return domain.AdminUser{}, err
	}
	s.log.InfoContext(ctx, "user suspended", "user_id", userID, "actor_id", actorID)
	s.publishUserRevocation(ctx, userID)
	return s.store.AdminUser(ctx, userID, now)
}

func (s *Service) RestoreUser(ctx context.Context, actorID, userID uuid.UUID, reason string) (domain.AdminUser, error) {
	now := s.now().UTC()
	action := s.adminAction(ctx, actorID, actionRestore, userID, reason)
	if _, err := s.store.RestoreUser(ctx, userID, now, action); err != nil {
		return domain.AdminUser{}, err
	}
	s.log.InfoContext(ctx, "user restored", "user_id", userID, "actor_id", actorID)
	return s.store.AdminUser(ctx, userID, now)
}

func (s *Service) RevokeUserSessions(ctx context.Context, actorID, userID uuid.UUID, reason string) (int64, error) {
	if actorID == userID {
		return 0, domain.ErrSelfAction
	}
	action := s.adminAction(ctx, actorID, actionSessions, userID, reason)
	revoked, err := s.store.RevokeUserSessions(ctx, userID, s.now().UTC(), action)
	if err != nil {
		return 0, err
	}
	s.log.InfoContext(ctx, "user sessions revoked", "user_id", userID, "actor_id", actorID, "revoked", revoked)
	s.publishUserRevocation(ctx, userID)
	return revoked, nil
}

func (s *Service) ListAuditEvents(ctx context.Context, f domain.AuditFilter) (domain.AuditPage, error) {
	return s.store.ListAuditEvents(ctx, f)
}

func (s *Service) RecordAdminAction(ctx context.Context, action authz.Action) error {
	return s.store.RecordAdminAction(ctx, action)
}

func (s *Service) adminAction(ctx context.Context, actorID uuid.UUID, name string, userID uuid.UUID, reason string) authz.Action {
	action := authz.NewAction(ctx, auditService, name, auditUser, userID.String(), reason)
	action.ActorID = actorID
	action.At = s.now().UTC()
	if action.ClientIP == "" && action.UserAgent == "" {
		device := domain.DeviceFromContext(ctx)
		action.ClientIP, action.UserAgent = device.IP, device.UserAgent
	}
	return action
}

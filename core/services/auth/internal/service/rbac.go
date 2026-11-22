package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const rbacNamespace = "rbac"

func (s *Service) UserByEmail(ctx context.Context, callerID uuid.UUID, email string) (domain.User, error) {
	if err := s.requirePermission(ctx, callerID, rbacNamespace, "read"); err != nil {
		return domain.User{}, err
	}

	user, err := s.store.UserByEmail(ctx, domain.NormalizeEmail(email))
	if err != nil {
		return domain.User{}, err
	}
	return s.attachRoles(ctx, user), nil
}

func (s *Service) ListRoles(ctx context.Context, callerID uuid.UUID) ([]domain.Role, error) {
	if err := s.requirePermission(ctx, callerID, rbacNamespace, "read"); err != nil {
		return nil, err
	}
	return s.store.ListRoles(ctx)
}

func (s *Service) AssignRole(ctx context.Context, callerID, userID, roleID uuid.UUID, reason string) error {
	return s.changeRole(ctx, callerID, userID, roleID, reason, true)
}

func (s *Service) RevokeRole(ctx context.Context, callerID, userID, roleID uuid.UUID, reason string) error {
	return s.changeRole(ctx, callerID, userID, roleID, reason, false)
}

func (s *Service) changeRole(ctx context.Context, callerID, userID, roleID uuid.UUID, reason string, grant bool) error {
	if err := s.requirePermission(ctx, callerID, rbacNamespace, "mutate"); err != nil {
		return err
	}
	if !grant && roleID == domain.RoleAdmin && callerID == userID {
		return domain.ErrSelfAction
	}

	name := actionRevoke
	if grant {
		name = actionGrant
	}
	action := s.adminAction(ctx, callerID, name, userID, reason)
	action.Details = map[string]string{"role_id": roleID.String()}
	if err := s.store.ChangeRole(ctx, userID, roleID, grant, action); err != nil {
		return err
	}

	s.log.InfoContext(ctx, "role changed", "user_id", userID, "role_id", roleID, "granted", grant, "actor_id", callerID)
	return nil
}

func (s *Service) requirePermission(ctx context.Context, userID uuid.UUID, namespace, action string) error {
	ok, err := s.store.UserHasPermission(ctx, userID, namespace, action)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrPermissionDenied
	}
	return nil
}

func (s *Service) attachRoles(ctx context.Context, user domain.User) domain.User {
	roles, err := s.store.RoleNamesForUser(ctx, user.ID)
	if err != nil {
		s.log.WarnContext(ctx, "could not load roles", "user_id", user.ID, "error", err)
		return user
	}
	user.Roles = roles
	return user
}

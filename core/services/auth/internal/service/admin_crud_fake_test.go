package service_test

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (s *fakeStore) ExportAuditEvents(_ context.Context, _ domain.AuditFilter, limit int) ([]domain.AuditEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.auditExport[:min(limit, len(s.auditExport))]), nil
}

func (s *fakeStore) PermissionCatalog(context.Context) ([]domain.Permission, error) {
	return []domain.Permission{{Namespace: "admin.users", Action: "read"}, {Namespace: "rbac", Action: "read"}}, nil
}

func (s *fakeStore) AdminRoles(context.Context) ([]domain.AdminRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.AdminRole{}
	for _, role := range s.adminRoles {
		out = append(out, role)
	}
	return out, nil
}

func (s *fakeStore) AdminRole(_ context.Context, id uuid.UUID) (domain.AdminRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	role, ok := s.adminRoles[id]
	if !ok {
		return domain.AdminRole{}, domain.ErrRoleNotFound
	}
	return role, nil
}

func (s *fakeStore) CreateRole(_ context.Context, role domain.AdminRole, at time.Time, action authz.Action, dryRun bool) (domain.AdminRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.adminRoles {
		if existing.Name == role.Name {
			return domain.AdminRole{}, domain.ErrRoleExists
		}
	}
	role.Version, role.CreatedAt, role.UpdatedAt = 1, at, at
	if dryRun {
		return role, nil
	}
	if s.adminRoles == nil {
		s.adminRoles = map[uuid.UUID]domain.AdminRole{}
	}
	s.adminRoles[role.ID] = role
	s.adminActions = append(s.adminActions, action)
	return role, nil
}

func (s *fakeStore) UpdateRole(_ context.Context, id uuid.UUID, version int64, fields domain.RoleFields, next domain.AdminRole, at time.Time, action authz.Action, diff func(before, after domain.AdminRole) []authz.Change, dryRun bool) (domain.AdminRole, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, ok := s.adminRoles[id]
	switch {
	case !ok:
		return domain.AdminRole{}, domain.ErrRoleNotFound
	case before.System:
		return domain.AdminRole{}, domain.ErrSystemRole
	case before.Version != version:
		return domain.AdminRole{}, &domain.VersionConflict{Current: before.Version}
	}
	after := before
	if fields.Title {
		after.Title = next.Title
	}
	if fields.Permissions {
		after.Permissions = next.Permissions
	}
	after.Version++
	after.UpdatedAt = at
	if dryRun {
		return after, nil
	}
	s.adminRoles[id] = after
	action.Changes = diff(before, after)
	s.adminActions = append(s.adminActions, action)
	return after, nil
}

func (s *fakeStore) DeleteRole(_ context.Context, id uuid.UUID, version int64, _ time.Time, action authz.Action, diff func(before, after domain.AdminRole) []authz.Change) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, ok := s.adminRoles[id]
	switch {
	case !ok:
		return domain.ErrRoleNotFound
	case before.Version != version:
		return &domain.VersionConflict{Current: before.Version}
	case before.Members > 0:
		return domain.ErrRoleInUse
	}
	delete(s.adminRoles, id)
	action.Changes = diff(before, domain.AdminRole{})
	s.adminActions = append(s.adminActions, action)
	return nil
}

func (s *fakeStore) SetUserRoles(_ context.Context, userID uuid.UUID, names []string, action authz.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return domain.ErrUserNotFound
	}
	granted := map[uuid.UUID]bool{}
	for _, name := range names {
		found := false
		for id, role := range s.roles {
			if role.Name == name {
				granted[id], found = true, true
			}
		}
		if !found {
			return domain.ErrRoleNotFound
		}
	}
	s.userRoles[userID] = granted
	s.adminActions = append(s.adminActions, action)
	return nil
}

func (s *fakeStore) UpdateUserDisplayName(_ context.Context, userID uuid.UUID, version int64, name string, at time.Time, action authz.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[userID]
	if !ok {
		return domain.ErrUserNotFound
	}
	if s.profileLocks == nil {
		s.profileLocks = map[uuid.UUID]int64{}
	}
	current := max(s.profileLocks[userID], 1)
	if current != version {
		return &domain.VersionConflict{Current: current}
	}
	action.Changes = []authz.Change{{Field: "display_name", Before: user.DisplayName, After: name}}
	user.DisplayName, user.UpdatedAt = name, at
	s.users[userID] = user
	s.profileLocks[userID] = current + 1
	s.adminActions = append(s.adminActions, action)
	return nil
}

func (s *fakeStore) LinkedAccounts(_ context.Context, userID uuid.UUID) ([]domain.LinkedAccount, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.LinkedAccount
	for _, account := range s.oauth {
		if account.UserID == userID {
			out = append(out, domain.LinkedAccount{Provider: account.Provider, Email: account.Email, LinkedAt: account.LinkedAt})
		}
	}
	_, password := s.credentials[userID]
	return out, password, nil
}

func (s *fakeStore) UnlinkOAuthAccount(_ context.Context, userID uuid.UUID, provider domain.Provider, action authz.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, password := s.credentials[userID]
	linked := 0
	key := ""
	for k, account := range s.oauth {
		if account.UserID == userID {
			linked++
			if account.Provider == provider {
				key = k
			}
		}
	}
	switch {
	case key == "":
		return domain.ErrLinkedAccountNotFound
	case !password && linked == 1:
		return domain.ErrOnlyLogin
	}
	delete(s.oauth, key)
	s.adminActions = append(s.adminActions, action)
	return nil
}

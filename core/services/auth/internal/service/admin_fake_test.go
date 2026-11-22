package service_test

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

var permissionChildren = map[string][]string{
	"manage": {"read", "mutate", "moderate"},
}

func (s *fakeStore) Permissions(_ context.Context, userID uuid.UUID) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("Permissions"); err != nil {
		return nil, err
	}
	out := []string{}
	if s.users[userID].Suspended() {
		return out, nil
	}
	seen := map[string]bool{}
	for roleID := range s.userRoles[userID] {
		for permission := range s.rolePermissions[roleID] {
			seen[permission] = true
			namespace, action, _ := strings.Cut(permission, ":")
			for _, child := range permissionChildren[action] {
				seen[namespace+":"+child] = true
			}
		}
	}
	for permission := range seen {
		out = append(out, permission)
	}
	sort.Strings(out)
	return out, nil
}

func (s *fakeStore) adminUserLocked(id uuid.UUID, at time.Time) (domain.AdminUser, error) {
	user, ok := s.users[id]
	if !ok {
		return domain.AdminUser{}, domain.ErrUserNotFound
	}
	var roles []string
	for roleID := range s.userRoles[id] {
		roles = append(roles, s.roles[roleID].Name)
	}
	sort.Strings(roles)
	user.Roles = roles
	families := map[uuid.UUID]bool{}
	for _, t := range s.tokens {
		if t.UserID == id && !t.Revoked() && !t.Expired(at) {
			families[t.FamilyID] = true
		}
	}
	return domain.AdminUser{User: user, ActiveSessions: len(families)}, nil
}

func (s *fakeStore) ListUsers(_ context.Context, f domain.UserFilter, at time.Time) (domain.UserPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	page := domain.UserPage{Users: []domain.AdminUser{}}
	for id := range s.users {
		user, err := s.adminUserLocked(id, at)
		if err != nil {
			return domain.UserPage{}, err
		}
		if f.Query != "" && !strings.Contains(user.User.Email, strings.ToLower(f.Query)) {
			continue
		}
		if f.Status == domain.UserStatusSuspended && !user.User.Suspended() || f.Status == domain.UserStatusActive && user.User.Suspended() {
			continue
		}
		page.Users = append(page.Users, user)
	}
	sort.Slice(page.Users, func(i, j int) bool { return page.Users[i].User.Email < page.Users[j].User.Email })
	page.Total = int64(len(page.Users))
	return page, nil
}

func (s *fakeStore) AdminUser(_ context.Context, id uuid.UUID, at time.Time) (domain.AdminUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.adminUserLocked(id, at)
}

func (s *fakeStore) activeAdminsExceptLocked(id uuid.UUID) (bool, int) {
	isAdmin := s.userRoles[id][domain.RoleAdmin]
	others := 0
	for userID, roles := range s.userRoles {
		if userID != id && roles[domain.RoleAdmin] && !s.users[userID].Suspended() {
			others++
		}
	}
	return isAdmin, others
}

func (s *fakeStore) SuspendUser(_ context.Context, userID uuid.UUID, reason string, at time.Time, action authz.Action) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.fail("SuspendUser"); err != nil {
		return domain.User{}, err
	}
	user, ok := s.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	if isAdmin, others := s.activeAdminsExceptLocked(userID); isAdmin && others == 0 {
		return domain.User{}, domain.ErrLastAdmin
	}
	if user.Suspended() {
		return domain.User{}, domain.ErrAlreadySuspended
	}
	user.SuspendedAt, user.SuspendedReason = &at, reason
	s.users[userID] = user
	for id, t := range s.tokens {
		if t.UserID == userID && t.RevokedAt.IsZero() {
			t.RevokedAt = at
			s.tokens[id] = t
		}
	}
	s.adminActions = append(s.adminActions, action)
	return user, nil
}

func (s *fakeStore) RestoreUser(_ context.Context, userID uuid.UUID, _ time.Time, action authz.Action) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[userID]
	if !ok {
		return domain.User{}, domain.ErrUserNotFound
	}
	if !user.Suspended() {
		return domain.User{}, domain.ErrNotSuspended
	}
	user.SuspendedAt, user.SuspendedReason = nil, ""
	s.users[userID] = user
	s.adminActions = append(s.adminActions, action)
	return user, nil
}

func (s *fakeStore) RevokeUserSessions(_ context.Context, userID uuid.UUID, at time.Time, action authz.Action) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[userID]; !ok {
		return 0, domain.ErrUserNotFound
	}
	var revoked int64
	for id, t := range s.tokens {
		if t.UserID == userID && t.RevokedAt.IsZero() {
			t.RevokedAt = at
			s.tokens[id] = t
			revoked++
		}
	}
	s.adminActions = append(s.adminActions, action)
	return revoked, nil
}

func (s *fakeStore) ChangeRole(ctx context.Context, userID, roleID uuid.UUID, grant bool, action authz.Action) error {
	s.mu.Lock()
	err := s.fail("RecordSecurityEvent")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if grant {
		if err := s.AssignRole(ctx, userID, roleID); err != nil {
			return err
		}
	} else {
		s.mu.Lock()
		if roleID == domain.RoleAdmin {
			if isAdmin, others := s.activeAdminsExceptLocked(userID); isAdmin && others == 0 {
				s.mu.Unlock()
				return domain.ErrLastAdmin
			}
		}
		s.mu.Unlock()
		if err := s.RevokeRole(ctx, userID, roleID); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminActions = append(s.adminActions, action)
	event := domain.SecurityEvent{Action: domain.SecurityRoleRevoked, UserID: userID, ActorID: action.ActorID, Subject: roleID.String(),
		Device: domain.Device{IP: action.ClientIP, UserAgent: action.UserAgent}, OccurredAt: action.At}
	if grant {
		event.Action = domain.SecurityRoleAssigned
	}
	s.securityEvents = append(s.securityEvents, event)
	return nil
}

func (s *fakeStore) RecordAdminAction(_ context.Context, action authz.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminActions = append(s.adminActions, action)
	return nil
}

func (s *fakeStore) ListAuditEvents(context.Context, domain.AuditFilter) (domain.AuditPage, error) {
	return domain.AuditPage{Events: []domain.AuditEvent{}}, nil
}

func (s *fakeStore) recordedAdminActions() []authz.Action {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]authz.Action(nil), s.adminActions...)
}

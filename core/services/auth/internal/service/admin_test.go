package service_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func TestPermissionsSeparateAdminFromUsers(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)
	moderator := h.register(t, "moderator@axon.test", validPassword).User.ID
	if err := h.store.AssignRole(t.Context(), moderator, domain.RoleModerator); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		user    uuid.UUID
		granted []authz.Permission
		denied  []authz.Permission
	}{
		{name: "user", user: target, denied: []authz.Permission{authz.UsersRead, authz.GamesModerate, authz.ChatRead, authz.AuditRead, authz.RolesRead}},
		{name: "moderator", user: moderator, granted: []authz.Permission{authz.UsersRead, authz.GamesModerate, authz.ChatModerate, authz.AuditRead, authz.RolesRead},
			denied: []authz.Permission{authz.UsersMutate, authz.RolesMutate}},
		{name: "admin", user: admin, granted: []authz.Permission{authz.UsersMutate, authz.GamesModerate, authz.ChatModerate, authz.AuditRead, authz.RolesMutate}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			granted, err := h.svc.Permissions(t.Context(), tt.user)
			if err != nil {
				t.Fatal(err)
			}
			set := authz.NewSet(granted)
			for _, p := range tt.granted {
				if !set.Has(p) {
					t.Errorf("%s is missing %s", tt.name, p)
				}
			}
			for _, p := range tt.denied {
				if set.Has(p) {
					t.Errorf("%s unexpectedly has %s", tt.name, p)
				}
			}
		})
	}
}

func TestSuspendUserBlocksSignInAndRecordsTheAction(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)
	session := h.register(t, "second@axon.test", validPassword)
	_ = session

	user, err := h.svc.SuspendUser(onDevice(t), admin, target, "spam in public rooms")
	if err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}
	if !user.User.Suspended() || user.User.SuspendedReason != "spam in public rooms" || user.ActiveSessions != 0 {
		t.Fatalf("suspended user = %+v", user)
	}

	if _, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "target@axon.test", Password: validPassword}); !errors.Is(err, domain.ErrAccountSuspended) {
		t.Fatalf("Login error = %v, want ErrAccountSuspended", err)
	}
	granted, err := h.svc.Permissions(t.Context(), target)
	if err != nil || len(granted) != 0 {
		t.Fatalf("suspended permissions = %v, %v", granted, err)
	}

	actions := h.store.recordedAdminActions()
	if len(actions) != 1 {
		t.Fatalf("recorded %d admin actions, want 1", len(actions))
	}
	got := actions[0]
	if got.Name != "user_suspended" || got.ActorID != admin || got.TargetID != target.String() || got.Reason != "spam in public rooms" ||
		got.ClientIP != auditDevice.IP || got.Service != "auth" || !got.At.Equal(h.now()) {
		t.Fatalf("action = %+v", got)
	}

	if _, err := h.svc.SuspendUser(onDevice(t), admin, target, "again"); !errors.Is(err, domain.ErrAlreadySuspended) {
		t.Fatalf("second suspend = %v", err)
	}

	restored, err := h.svc.RestoreUser(onDevice(t), admin, target, "appeal accepted")
	if err != nil || restored.User.Suspended() {
		t.Fatalf("RestoreUser = %+v, %v", restored, err)
	}
	if _, err := h.svc.Login(onDevice(t), service.LoginInput{Email: "target@axon.test", Password: validPassword}); err != nil {
		t.Fatalf("Login after restore: %v", err)
	}
}

func TestRefreshFailsForASuspendedAccount(t *testing.T) {
	h := newHarness(t)
	admin, _ := adminAndTarget(t, h)
	victim := h.register(t, "victim@axon.test", validPassword)

	h.store.mu.Lock()
	user := h.store.users[victim.User.ID]
	now := h.now()
	user.SuspendedAt = &now
	h.store.users[victim.User.ID] = user
	h.store.mu.Unlock()
	_ = admin

	if _, err := h.svc.Refresh(t.Context(), victim.Tokens.RefreshToken, domain.Device{}); !errors.Is(err, domain.ErrRefreshTokenInvalid) {
		t.Fatalf("Refresh = %v, want ErrRefreshTokenInvalid", err)
	}
}

func TestAdminCannotLockThemselvesOut(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)

	if _, err := h.svc.SuspendUser(t.Context(), admin, admin, "testing"); !errors.Is(err, domain.ErrSelfAction) {
		t.Fatalf("self suspend = %v", err)
	}
	if _, err := h.svc.RevokeUserSessions(t.Context(), admin, admin, "testing"); !errors.Is(err, domain.ErrSelfAction) {
		t.Fatalf("self revoke sessions = %v", err)
	}
	if err := h.svc.RevokeRole(t.Context(), admin, admin, domain.RoleAdmin, "testing"); !errors.Is(err, domain.ErrSelfAction) {
		t.Fatalf("self revoke admin = %v", err)
	}

	if err := h.svc.AssignRole(t.Context(), admin, target, domain.RoleAdmin, "second admin"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RevokeRole(t.Context(), target, admin, domain.RoleAdmin, "rotation"); err != nil {
		t.Fatalf("revoke the other admin: %v", err)
	}
	if _, err := h.svc.SuspendUser(t.Context(), admin, target, "last one"); !errors.Is(err, domain.ErrLastAdmin) {
		t.Fatalf("suspend the last admin = %v", err)
	}
}

func TestRoleChangeIsAtomicWithItsAudit(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)
	h.store.failOn["RecordSecurityEvent"] = errors.New("outbox unavailable")

	if err := h.svc.AssignRole(onDevice(t), admin, target, domain.RoleModerator, "support rota"); err == nil {
		t.Fatal("AssignRole succeeded although its audit could not be written")
	}
	roles, err := h.store.RoleNamesForUser(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(roles, "moderator") {
		t.Fatalf("roles = %v, the change must roll back with its audit", roles)
	}
	if len(h.store.recordedAdminActions()) != 0 {
		t.Fatal("an admin action was recorded for a rolled back change")
	}
}

func TestRevokeUserSessions(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)
	if _, err := h.svc.Login(t.Context(), service.LoginInput{Email: "target@axon.test", Password: validPassword}); err != nil {
		t.Fatal(err)
	}
	revoked, err := h.svc.RevokeUserSessions(onDevice(t), admin, target, "lost device")
	if err != nil || revoked != 2 {
		t.Fatalf("RevokeUserSessions = %d, %v", revoked, err)
	}
	user, _, err := h.svc.AdminUser(t.Context(), admin, target)
	if err != nil || user.ActiveSessions != 0 {
		t.Fatalf("AdminUser = %+v, %v", user, err)
	}
	actions := h.store.recordedAdminActions()
	if len(actions) != 2 || actions[0].Name != "user_sessions_revoked" || actions[1].Name != "user_viewed" || actions[1].ActorID != admin || actions[1].TargetID != target.String() {
		t.Fatalf("actions = %+v, want the revocation and then the view", actions)
	}
}

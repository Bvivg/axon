package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestRegisterGrantsTheDefaultUserRole(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	if len(registered.User.Roles) != 1 || registered.User.Roles[0] != "user" {
		t.Errorf("roles = %v, want [user]", registered.User.Roles)
	}
}

func TestMeReflectsRoles(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), registered.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	user, err := h.svc.Me(context.Background(), registered.User.ID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if len(user.Roles) != 2 {
		t.Fatalf("roles = %v, want 2 roles", user.Roles)
	}
}

func TestAttachRolesDegradesWhenTheStoreFails(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	h.store.failOn["RoleNamesForUser"] = errors.New("boom")

	user, err := h.svc.Me(context.Background(), registered.User.ID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if user.Roles != nil {
		t.Errorf("roles = %v, want nil when the store fails", user.Roles)
	}
}

func TestUserByEmailRequiresRbacRead(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	h.register(t, "eve@example.com", validPassword)

	_, err := h.svc.UserByEmail(context.Background(), caller.User.ID, "eve@example.com")
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestUserByEmailSucceedsForAnAdmin(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t, "admin@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), admin.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	found, err := h.svc.UserByEmail(context.Background(), admin.User.ID, "eve@example.com")
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if found.ID != target.User.ID {
		t.Errorf("found user %s, want %s", found.ID, target.User.ID)
	}
	if len(found.Roles) != 1 || found.Roles[0] != "user" {
		t.Errorf("roles = %v, want [user]", found.Roles)
	}
}

func TestUserByEmailUnknownReturnsErrUserNotFound(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t, "admin@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), admin.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	_, err := h.svc.UserByEmail(context.Background(), admin.User.ID, "nobody@example.com")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestListRolesRequiresRbacRead(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	_, err := h.svc.ListRoles(context.Background(), registered.User.ID)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestListRolesSucceedsForAnAdmin(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t, "admin@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), admin.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	roles, err := h.svc.ListRoles(context.Background(), admin.User.ID)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("got %d roles, want 2 (user, admin)", len(roles))
	}
}

func TestAssignRoleRequiresRbacMutate(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	err := h.svc.AssignRole(context.Background(), caller.User.ID, target.User.ID, domain.RoleAdmin)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

func TestAdminCanAssignAndRevokeRoles(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t, "admin@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), admin.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	if err := h.svc.AssignRole(context.Background(), admin.User.ID, target.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	names, err := h.store.RoleNamesForUser(context.Background(), target.User.ID)
	if err != nil {
		t.Fatalf("RoleNamesForUser: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("target roles = %v, want 2", names)
	}

	if err := h.svc.RevokeRole(context.Background(), admin.User.ID, target.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("RevokeRole: %v", err)
	}

	names, err = h.store.RoleNamesForUser(context.Background(), target.User.ID)
	if err != nil {
		t.Fatalf("RoleNamesForUser: %v", err)
	}
	if len(names) != 1 || names[0] != "user" {
		t.Fatalf("target roles after revoke = %v, want [user]", names)
	}
}

func TestRevokeRoleUnknownReturnsErrRoleNotFound(t *testing.T) {
	h := newHarness(t)
	admin := h.register(t, "admin@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	if err := h.store.AssignRole(context.Background(), admin.User.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("assign admin directly: %v", err)
	}

	err := h.svc.RevokeRole(context.Background(), admin.User.ID, target.User.ID, domain.RoleAdmin)
	if !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRevokeRoleRequiresRbacMutate(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)

	err := h.svc.RevokeRole(context.Background(), caller.User.ID, uuid.New(), domain.RoleUser)
	if !errors.Is(err, domain.ErrPermissionDenied) {
		t.Fatalf("err = %v, want ErrPermissionDenied", err)
	}
}

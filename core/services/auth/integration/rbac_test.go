//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestCreateUserWithPasswordAssignsTheDefaultRole(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	names, err := repo.RoleNamesForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("RoleNamesForUser: %v", err)
	}
	if len(names) != 1 || names[0] != "user" {
		t.Fatalf("roles = %v, want [user]", names)
	}
}

func TestCreateUserWithOauthAccountAssignsTheDefaultRole(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	account := domain.OauthAccount{
		Provider:       domain.ProviderGoogle,
		ProviderUserID: "google-" + uuid.NewString(),
		Email:          "oauth-" + uuid.NewString() + "@axon.test",
	}

	created, err := repo.CreateUserWithOauthAccount(ctx, domain.User{
		ID:    uuid.New(),
		Email: account.Email,
	}, account)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	names, err := repo.RoleNamesForUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("RoleNamesForUser: %v", err)
	}
	if len(names) != 1 || names[0] != "user" {
		t.Fatalf("roles = %v, want [user]", names)
	}
}

func TestUserHasPermissionResolvesThroughTheManageHierarchy(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	for _, action := range []string{"read", "mutate", "manage"} {
		ok, err := repo.UserHasPermission(ctx, user.ID, "chat", action)
		if err != nil {
			t.Fatalf("UserHasPermission(chat, %s): %v", action, err)
		}
		if !ok {
			t.Errorf("a default user should have chat:%s via the manage root", action)
		}
	}

	ok, err := repo.UserHasPermission(ctx, user.ID, "rbac", "read")
	if err != nil {
		t.Fatalf("UserHasPermission(rbac, read): %v", err)
	}
	if ok {
		t.Error("a default user should not have rbac:read")
	}
}

func TestUserHasPermissionForAdminCoversRbac(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	if err := repo.AssignRole(ctx, user.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("AssignRole(admin): %v", err)
	}

	ok, err := repo.UserHasPermission(ctx, user.ID, "rbac", "mutate")
	if err != nil {
		t.Fatalf("UserHasPermission(rbac, mutate): %v", err)
	}
	if !ok {
		t.Error("an admin should have rbac:mutate via the manage root")
	}
}

func TestUserHasPermissionCoversACustomAction(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	ok, err := repo.UserHasPermission(ctx, user.ID, "game", "moderate")
	if err != nil {
		t.Fatalf("UserHasPermission(game, moderate): %v", err)
	}
	if !ok {
		t.Error("a default user should have the custom game:moderate action via the manage root")
	}
}

func TestAssignRoleIsIdempotent(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	if err := repo.AssignRole(ctx, user.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("first assign: %v", err)
	}
	if err := repo.AssignRole(ctx, user.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("second assign: %v", err)
	}

	names, err := repo.RoleNamesForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("RoleNamesForUser: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("roles = %v, want exactly [admin user]", names)
	}
}

func TestAssignRoleUnknownRoleReturnsErrRoleNotFound(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	err := repo.AssignRole(ctx, user.ID, uuid.New())
	if !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRevokeRoleUnassignedReturnsErrRoleNotFound(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	err := repo.RevokeRole(ctx, user.ID, domain.RoleAdmin)
	if !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("err = %v, want ErrRoleNotFound", err)
	}
}

func TestRevokeRoleRemovesAccess(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)
	if err := repo.AssignRole(ctx, user.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	if err := repo.RevokeRole(ctx, user.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("RevokeRole: %v", err)
	}

	ok, err := repo.UserHasPermission(ctx, user.ID, "rbac", "read")
	if err != nil {
		t.Fatalf("UserHasPermission: %v", err)
	}
	if ok {
		t.Error("rbac:read should be gone after revoking the admin role")
	}
}

func TestListRolesReturnsTheSeededCatalog(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	roles, err := repo.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}

	byName := make(map[string]int)
	for _, r := range roles {
		byName[r.Name] = len(r.Permissions)
	}

	if byName["user"] == 0 {
		t.Error("the user role has no permissions attached")
	}
	if byName["admin"] == 0 {
		t.Error("the admin role has no permissions attached")
	}
}

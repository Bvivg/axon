//go:build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/config"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

const adminRoleID = "00000000-0000-0000-0000-000000000002"

func (c *caller) getUserByEmail(accessToken, email string) (*authv1.User, error) {
	r := connect.NewRequest(&authv1.GetUserByEmailRequest{Email: email})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.GetUserByEmail(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUser(), nil
}

func (c *caller) listRoles(accessToken string) ([]*authv1.Role, error) {
	r := connect.NewRequest(&authv1.ListRolesRequest{})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := c.client.ListRoles(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetRoles(), nil
}

func (c *caller) assignRole(accessToken, userID, roleID string) error {
	r := connect.NewRequest(&authv1.AssignRoleRequest{UserId: userID, RoleId: roleID})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	_, err := c.client.AssignRole(context.Background(), r)
	return err
}

func (c *caller) revokeRole(accessToken, userID, roleID string) error {
	r := connect.NewRequest(&authv1.RevokeRoleRequest{UserId: userID, RoleId: roleID})
	r.Header().Set(authn.Header, "Bearer "+accessToken)

	_, err := c.client.RevokeRole(context.Background(), r)
	return err
}

func openDBPool(t *testing.T) *postgres.Pool {
	t.Helper()

	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN is not set; run via make test-e2e")
	}

	pool, err := postgres.Connect(context.Background(), postgres.Config{
		DSN: config.NewSecret(dsn),
	}, logger.Discard())
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func grantAdminDirectly(t *testing.T, userID string) {
	t.Helper()

	pool := openDBPool(t)

	_, err := pool.Exec(t.Context(),
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, adminRoleID)
	if err != nil {
		t.Fatalf("grant admin directly: %v", err)
	}
}

func TestRegisterGrantsTheDefaultUserRole(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	user, err := c.getMe(acct.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if len(user.GetRoles()) != 1 || user.GetRoles()[0] != "user" {
		t.Errorf("roles = %v, want [user]", user.GetRoles())
	}
}

func TestListRolesRequiresAnAdmin(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	_, err := c.listRoles(acct.tokens.GetAccessToken())
	requireCode(t, err, connect.CodePermissionDenied)
}

func TestAdminCanListAssignAndRevokeRoles(t *testing.T) {
	c := newCaller(t)
	admin := c.register(t)
	target := c.register(t)

	grantAdminDirectly(t, admin.userID)

	roles, err := c.listRoles(admin.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}

	byName := make(map[string]string)
	for _, r := range roles {
		byName[r.GetName()] = r.GetId()
	}
	if _, ok := byName["admin"]; !ok {
		t.Fatal("the admin role is missing from the catalog")
	}
	if _, ok := byName["user"]; !ok {
		t.Fatal("the user role is missing from the catalog")
	}

	if err := c.assignRole(admin.tokens.GetAccessToken(), target.userID, byName["admin"]); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	targetUser, err := c.getMe(target.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if len(targetUser.GetRoles()) != 2 {
		t.Fatalf("target roles = %v, want 2", targetUser.GetRoles())
	}

	if err := c.revokeRole(admin.tokens.GetAccessToken(), target.userID, byName["admin"]); err != nil {
		t.Fatalf("RevokeRole: %v", err)
	}

	targetUser, err = c.getMe(target.tokens.GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if len(targetUser.GetRoles()) != 1 || targetUser.GetRoles()[0] != "user" {
		t.Fatalf("target roles after revoke = %v, want [user]", targetUser.GetRoles())
	}
}

func TestGetUserByEmailRequiresAnAdmin(t *testing.T) {
	c := newCaller(t)
	caller := c.register(t)
	target := c.register(t)

	_, err := c.getUserByEmail(caller.tokens.GetAccessToken(), target.email)
	requireCode(t, err, connect.CodePermissionDenied)
}

func TestAdminCanLookUpAUserByEmail(t *testing.T) {
	c := newCaller(t)
	admin := c.register(t)
	target := c.register(t)

	grantAdminDirectly(t, admin.userID)

	found, err := c.getUserByEmail(admin.tokens.GetAccessToken(), target.email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if found.GetId() != target.userID {
		t.Errorf("found id %s, want %s", found.GetId(), target.userID)
	}
	if len(found.GetRoles()) != 1 || found.GetRoles()[0] != "user" {
		t.Errorf("roles = %v, want [user]", found.GetRoles())
	}
}

func TestAssignRoleCannotBeCalledByANonAdmin(t *testing.T) {
	c := newCaller(t)
	attacker := c.register(t)
	target := c.register(t)

	err := c.assignRole(attacker.tokens.GetAccessToken(), target.userID, adminRoleID)
	requireCode(t, err, connect.CodePermissionDenied)
}

//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const adminReason = "e2e moderation check"

func (c *caller) admin() authv1connect.AuthAdminServiceClient {
	return authv1connect.NewAuthAdminServiceClient(&http.Client{Transport: forwardedFor{addr: c.addr}}, gatewayURL)
}

func signed[T any](token string, msg *T) *connect.Request[T] {
	r := connect.NewRequest(msg)
	r.Header().Set(authn.Header, "Bearer "+token)
	return r
}

func grantRoleDirectly(t *testing.T, userID, role string) {
	t.Helper()
	pool := openDBPool(t)
	tag, err := pool.Exec(t.Context(), `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = $2 ON CONFLICT DO NOTHING`, userID, role)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("grant %s directly: %v (%d rows)", role, err, tag.RowsAffected())
	}
}

func TestOnlyStaffReachTheAdminConsole(t *testing.T) {
	c := newCaller(t)
	plain := c.register(t)

	permissions, err := c.admin().GetMyPermissions(context.Background(), signed(plain.tokens.GetAccessToken(), &authv1.GetMyPermissionsRequest{}))
	if err != nil {
		t.Fatalf("GetMyPermissions: %v", err)
	}
	if slices.ContainsFunc(permissions.Msg.GetPermissions(), func(p string) bool { return len(p) > 6 && p[:6] == "admin." }) {
		t.Errorf("a plain user holds %v", permissions.Msg.GetPermissions())
	}

	_, err = c.admin().ListUsers(context.Background(), signed(plain.tokens.GetAccessToken(), &authv1.ListUsersRequest{}))
	requireCode(t, err, connect.CodePermissionDenied)

	_, err = c.admin().ListUsers(context.Background(), connect.NewRequest(&authv1.ListUsersRequest{}))
	requireCode(t, err, connect.CodeUnauthenticated)
}

func TestAModeratorReadsAccountsButCannotSuspendThem(t *testing.T) {
	c := newCaller(t)
	moderator := c.register(t)
	target := c.register(t)
	grantRoleDirectly(t, moderator.userID, "moderator")
	token := moderator.tokens.GetAccessToken()

	found, err := c.admin().ListUsers(context.Background(), signed(token, &authv1.ListUsersRequest{Query: target.email}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if found.Msg.GetTotal() != 1 || len(found.Msg.GetUsers()) != 1 || found.Msg.GetUsers()[0].GetUser().GetId() != target.userID {
		t.Fatalf("search by email = %v, want the target", found.Msg.GetUsers())
	}

	_, err = c.admin().SuspendUser(context.Background(), signed(token, &authv1.SuspendUserRequest{UserId: target.userID, Reason: adminReason}))
	requireCode(t, err, connect.CodePermissionDenied)
}

func TestAnAdminSuspendsRestoresAndSignsAnAccountOut(t *testing.T) {
	c := newCaller(t)
	admin := c.register(t)
	target := c.register(t)
	grantAdminDirectly(t, admin.userID)
	token := admin.tokens.GetAccessToken()
	console := c.admin()

	_, err := console.SuspendUser(context.Background(), signed(token, &authv1.SuspendUserRequest{UserId: target.userID, Reason: "x"}))
	requireCode(t, err, connect.CodeInvalidArgument)

	_, err = console.SuspendUser(context.Background(), signed(token, &authv1.SuspendUserRequest{UserId: admin.userID, Reason: adminReason}))
	requireCode(t, err, connect.CodeFailedPrecondition)

	suspended, err := console.SuspendUser(context.Background(), signed(token, &authv1.SuspendUserRequest{UserId: target.userID, Reason: adminReason}))
	if err != nil {
		t.Fatalf("SuspendUser: %v", err)
	}
	if suspended.Msg.GetUser().GetStatus() != authv1.UserStatus_USER_STATUS_SUSPENDED || suspended.Msg.GetUser().GetSuspendedReason() != adminReason {
		t.Errorf("suspended user = %v", suspended.Msg.GetUser())
	}

	_, err = c.login(target.email, testPassword)
	requireCode(t, err, connect.CodePermissionDenied)
	_, err = c.refresh(target.tokens.GetRefreshToken())
	requireCode(t, err, connect.CodeUnauthenticated)

	listed, err := console.ListUsers(context.Background(), signed(token, &authv1.ListUsersRequest{Query: target.email, Status: authv1.UserStatus_USER_STATUS_SUSPENDED}))
	if err != nil || listed.Msg.GetTotal() != 1 {
		t.Fatalf("suspended filter = %v, %v", listed, err)
	}

	if _, err := console.RestoreUser(context.Background(), signed(token, &authv1.RestoreUserRequest{UserId: target.userID, Reason: adminReason})); err != nil {
		t.Fatalf("RestoreUser: %v", err)
	}
	if _, err := c.login(target.email, testPassword); err != nil {
		t.Fatalf("login after restore: %v", err)
	}

	detail, err := console.GetUser(context.Background(), signed(token, &authv1.GetUserRequest{UserId: target.userID}))
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if len(detail.Msg.GetSessions()) == 0 {
		t.Fatal("the restored account has no session after signing in")
	}

	revoked, err := console.RevokeUserSessions(context.Background(), signed(token, &authv1.RevokeUserSessionsRequest{UserId: target.userID, Reason: adminReason}))
	if err != nil || revoked.Msg.GetRevoked() == 0 {
		t.Fatalf("RevokeUserSessions = %v, %v", revoked, err)
	}
	after, err := console.GetUser(context.Background(), signed(token, &authv1.GetUserRequest{UserId: target.userID}))
	if err != nil || len(after.Msg.GetSessions()) != 0 {
		t.Errorf("sessions after revoking = %v, %v", after.Msg.GetSessions(), err)
	}
}

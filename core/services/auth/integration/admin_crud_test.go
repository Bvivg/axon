//go:build integration

package integration

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func roleDiff(before, after domain.AdminRole) []authz.Change {
	return []authz.Change{{Field: "title", Before: before.Title, After: after.Title}}
}

func crudAction(name, targetType, targetID string) authz.Action {
	return authz.Action{Service: "auth", Name: name, Outcome: authz.Succeeded, ActorID: uuid.New(), TargetType: targetType, TargetID: targetID, Reason: "integration", At: time.Now().UTC()}
}

func outboxCount(t *testing.T, targetID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox WHERE aggregatetype = 'admin.audit.v1' AND aggregateid = $1`, targetID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRoleLifecycle(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	id := uuid.New()
	name := "ops_" + id.String()[:8]
	at := time.Now().UTC()

	if _, err := repo.CreateRole(ctx, domain.AdminRole{ID: id, Name: name, Title: "Ops", Permissions: []string{"rbac:read"}}, at, crudAction("role_created", "role", id.String()), true); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if _, err := repo.AdminRole(ctx, id); !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("dry run stored the role: %v", err)
	}

	created, err := repo.CreateRole(ctx, domain.AdminRole{ID: id, Name: name, Title: "Ops", Permissions: []string{"rbac:read", "admin.users:read"}}, at, crudAction("role_created", "role", id.String()), false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Version != 1 || created.System || !slices.Equal(created.Permissions, []string{"admin.users:read", "rbac:read"}) {
		t.Fatalf("created = %+v", created)
	}
	replayed, err := repo.CreateRole(ctx, domain.AdminRole{ID: id, Name: name, Title: "Ops"}, at, crudAction("role_created", "role", id.String()), false)
	if err != nil || replayed.ID != id || outboxCount(t, id.String()) != 1 {
		t.Fatalf("replay = %+v, %v, outbox %d", replayed, err, outboxCount(t, id.String()))
	}
	if _, err := repo.CreateRole(ctx, domain.AdminRole{ID: id, Name: name + "x", Title: "Other"}, at, crudAction("role_created", "role", id.String()), false); !errors.Is(err, domain.ErrRequestReused) {
		t.Fatalf("reused request id: %v", err)
	}
	if _, err := repo.CreateRole(ctx, domain.AdminRole{ID: uuid.New(), Name: name, Title: "Dup"}, at, crudAction("role_created", "role", "dup"), false); !errors.Is(err, domain.ErrRoleExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := repo.CreateRole(ctx, domain.AdminRole{ID: uuid.New(), Name: name + "_p", Title: "Bad", Permissions: []string{"nope:read"}}, at, crudAction("role_created", "role", "bad"), false); !errors.Is(err, domain.ErrUnknownPermission) {
		t.Fatalf("unknown permission: %v", err)
	}

	if _, err := repo.UpdateRole(ctx, id, 5, domain.RoleFields{Title: true}, domain.AdminRole{Title: "Operations"}, at, crudAction("role_updated", "role", id.String()), roleDiff, false); err == nil {
		t.Fatal("stale version updated the role")
	} else if conflict, ok := domain.AsVersionConflict(err); !ok || conflict.Current != 1 {
		t.Fatalf("stale version: %v", err)
	}
	updated, err := repo.UpdateRole(ctx, id, 1, domain.RoleFields{Title: true, Permissions: true}, domain.AdminRole{Title: "Operations", Permissions: []string{"rbac:read"}}, at, crudAction("role_updated", "role", id.String()), roleDiff, false)
	if err != nil || updated.Version != 2 || updated.Title != "Operations" || !slices.Equal(updated.Permissions, []string{"rbac:read"}) {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	again, err := repo.UpdateRole(ctx, id, 1, domain.RoleFields{Title: true}, domain.AdminRole{Title: "Operations"}, at, crudAction("role_updated", "role", id.String()), roleDiff, false)
	if err != nil || again.Version != 2 {
		t.Fatalf("retried update = %+v, %v", again, err)
	}

	user := createUser(t, repo)
	if err := repo.SetUserRoles(ctx, user.ID, []string{"user", name}, crudAction("user_roles_changed", "user", user.ID.String())); err != nil {
		t.Fatalf("set roles: %v", err)
	}
	if err := repo.DeleteRole(ctx, id, 2, at, crudAction("role_deleted", "role", id.String()), roleDiff); !errors.Is(err, domain.ErrRoleInUse) {
		t.Fatalf("delete with members: %v", err)
	}
	if err := repo.SetUserRoles(ctx, user.ID, []string{"user"}, crudAction("user_roles_changed", "user", user.ID.String())); err != nil {
		t.Fatalf("drop role: %v", err)
	}
	if err := repo.DeleteRole(ctx, id, 2, at, crudAction("role_deleted", "role", id.String()), roleDiff); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var kept int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM deleted_records WHERE kind = 'role' AND record_id = $1 AND record->>'name' = $2`, id, name).Scan(&kept); err != nil || kept != 1 {
		t.Fatalf("deleted record = %d, %v", kept, err)
	}
	if err := repo.DeleteRole(ctx, domain.RoleAdmin, 1, at, crudAction("role_deleted", "role", "admin"), roleDiff); !errors.Is(err, domain.ErrSystemRole) {
		t.Fatalf("delete admin role: %v", err)
	}
}

func TestDisplayNameVersionMovesWithEveryProfileChange(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	user := createUser(t, repo)
	at := time.Now().UTC()

	admin, err := repo.AdminUser(ctx, user.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateUserDisplayName(ctx, user.ID, admin.Version, "Moderated", at, crudAction("user_profile_changed", "user", user.ID.String())); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := repo.UpdateUserDisplayName(ctx, user.ID, admin.Version, "Moderated", at, crudAction("user_profile_changed", "user", user.ID.String())); err != nil {
		t.Fatalf("retried rename: %v", err)
	}
	if err := repo.UpdateUserDisplayName(ctx, user.ID, admin.Version, "Other", at, crudAction("user_profile_changed", "user", user.ID.String())); err == nil {
		t.Fatal("stale version renamed the user")
	}
	if err := repo.UpdateLastSeen(ctx, user.ID, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	after, err := repo.AdminUser(ctx, user.ID, at)
	if err != nil || after.Version != admin.Version+1 || after.User.DisplayName != "Moderated" {
		t.Fatalf("after = %+v, %v", after, err)
	}
}

func TestUnlinkKeepsTheLastWayToSignIn(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	user := createUser(t, repo)
	if _, err := pool.Exec(ctx, `DELETE FROM credentials WHERE user_id = $1`, user.ID); err != nil {
		t.Fatal(err)
	}
	link := func(provider domain.Provider) {
		if err := repo.LinkOauthAccount(ctx, domain.OauthAccount{Provider: provider, ProviderUserID: uuid.NewString(), UserID: user.ID, Email: user.Email, LinkedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	link(domain.ProviderGitHub)
	link(domain.ProviderGoogle)

	if err := repo.UnlinkOAuthAccount(ctx, user.ID, domain.ProviderGoogle, crudAction("oauth_account_unlinked", "user", user.ID.String())); err != nil {
		t.Fatalf("unlink google: %v", err)
	}
	if err := repo.UnlinkOAuthAccount(ctx, user.ID, domain.ProviderGitHub, crudAction("oauth_account_unlinked", "user", user.ID.String())); !errors.Is(err, domain.ErrOnlyLogin) {
		t.Fatalf("unlink the last login: %v", err)
	}
	if err := repo.UnlinkOAuthAccount(ctx, user.ID, domain.ProviderGoogle, crudAction("oauth_account_unlinked", "user", user.ID.String())); !errors.Is(err, domain.ErrLinkedAccountNotFound) {
		t.Fatalf("unlink twice: %v", err)
	}
	accounts, password, err := repo.LinkedAccounts(ctx, user.ID)
	if err != nil || password || len(accounts) != 1 || accounts[0].Provider != domain.ProviderGitHub {
		t.Fatalf("accounts = %+v, password = %v, %v", accounts, password, err)
	}
}

func TestAuditChangesRoundTripAndExport(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	target := uuid.NewString()
	event := domain.AuditEvent{ID: uuid.New(), OccurredAt: time.Now().UTC(), Service: "auth", Action: "role_updated", Outcome: domain.AuditSucceeded,
		TargetType: "role", TargetID: target, Changes: []domain.AuditChange{{Field: "title", Before: "Ops", After: "Operations"}}}
	if _, err := repo.InsertAuditEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListAuditEvents(ctx, domain.AuditFilter{TargetType: "role", TargetID: target})
	if err != nil || len(page.Events) != 1 || len(page.Events[0].Changes) != 1 || page.Events[0].Changes[0].After != "Operations" {
		t.Fatalf("list = %+v, %v", page, err)
	}
	exported, err := repo.ExportAuditEvents(ctx, domain.AuditFilter{TargetType: "role", TargetID: target}, 10)
	if err != nil || len(exported) != 1 || exported[0].Changes[0].Before != "Ops" {
		t.Fatalf("export = %+v, %v", exported, err)
	}
}

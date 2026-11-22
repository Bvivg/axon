//go:build integration

package integration

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestAuditLogIsAppendOnly(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	actor := uuid.New()
	event := domain.AuditEvent{ID: uuid.New(), OccurredAt: time.Now().UTC(), Service: "game", Action: "session_cancelled",
		Outcome: domain.AuditSucceeded, ActorID: &actor, TargetType: "game_session", TargetID: "s-1", Details: map[string]string{"game": "chess"}}

	inserted, err := repo.InsertAuditEvent(ctx, event)
	if err != nil || !inserted {
		t.Fatalf("insert = %v, %v", inserted, err)
	}
	inserted, err = repo.InsertAuditEvent(ctx, event)
	if err != nil || inserted {
		t.Fatalf("duplicate insert = %v, %v", inserted, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE audit_log SET reason = 'edited' WHERE id = $1`, event.ID); err == nil {
		t.Fatal("UPDATE on audit_log succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audit_log WHERE id = $1`, event.ID); err == nil {
		t.Fatal("DELETE on audit_log succeeded")
	}
	if _, err := pool.Exec(ctx, `TRUNCATE audit_log`); err == nil {
		t.Fatal("TRUNCATE on audit_log succeeded")
	}

	page, err := repo.ListAuditEvents(ctx, domain.AuditFilter{TargetType: "game_session", TargetID: "s-1"})
	if err != nil || len(page.Events) != 1 || page.Events[0].Details["game"] != "chess" || *page.Events[0].ActorID != actor {
		t.Fatalf("list = %+v, %v", page, err)
	}
}

func TestListAuditEventsPagesByCursor(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	target := uuid.NewString()
	base := time.Now().UTC().Truncate(time.Millisecond)
	for i := range 5 {
		if _, err := repo.InsertAuditEvent(ctx, domain.AuditEvent{ID: uuid.New(), OccurredAt: base.Add(time.Duration(i) * time.Second),
			Service: "auth", Action: "user_suspended", Outcome: domain.AuditSucceeded, TargetType: "user", TargetID: target}); err != nil {
			t.Fatal(err)
		}
	}
	var seen []time.Time
	cursor := ""
	for {
		page, err := repo.ListAuditEvents(ctx, domain.AuditFilter{TargetID: target, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Events {
			seen = append(seen, e.OccurredAt)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != 5 {
		t.Fatalf("paged %d events, want 5", len(seen))
	}
	for i := 1; i < len(seen); i++ {
		if !seen[i].Before(seen[i-1]) {
			t.Fatalf("events are not newest first: %v", seen)
		}
	}
}

func TestListUsersFiltersAndPages(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	var created []domain.User
	for range 5 {
		created = append(created, createUser(t, repo))
	}
	if err := repo.AssignRole(ctx, created[0].ID, domain.RoleModerator); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SuspendUser(ctx, created[1].ID, "spam", now, authz.Action{Service: "auth", Name: "user_suspended", At: now}); err != nil {
		t.Fatal(err)
	}

	page, err := repo.ListUsers(ctx, domain.UserFilter{Role: "moderator"}, now)
	if err != nil || page.Total != 1 || page.Users[0].User.ID != created[0].ID {
		t.Fatalf("moderators = %+v, %v", page, err)
	}
	page, err = repo.ListUsers(ctx, domain.UserFilter{Status: domain.UserStatusSuspended}, now)
	if err != nil || page.Total != 1 || page.Users[0].User.SuspendedReason != "spam" {
		t.Fatalf("suspended = %+v, %v", page, err)
	}
	page, err = repo.ListUsers(ctx, domain.UserFilter{Query: created[2].Email[:12]}, now)
	if err != nil || page.Total != 1 {
		t.Fatalf("query = %+v, %v", page, err)
	}

	for _, sort := range []domain.UserSort{domain.UserSortNewest, domain.UserSortOldest, domain.UserSortEmail} {
		seen := map[uuid.UUID]bool{}
		cursor := ""
		for {
			page, err := repo.ListUsers(ctx, domain.UserFilter{Sort: sort, Cursor: cursor, Limit: 2}, now)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 5 {
				t.Fatalf("total = %d", page.Total)
			}
			for _, u := range page.Users {
				if seen[u.User.ID] {
					t.Fatalf("sort %d returned %s twice", sort, u.User.ID)
				}
				seen[u.User.ID] = true
			}
			if page.Next == "" {
				break
			}
			cursor = page.Next
		}
		if len(seen) != 5 {
			t.Fatalf("sort %d paged %d users, want 5", sort, len(seen))
		}
	}

	if _, err := repo.ListUsers(ctx, domain.UserFilter{Cursor: "%%%"}, now); err == nil {
		t.Fatal("a broken cursor was accepted")
	}
}

func TestSuspendRevokesTokensAndPermissions(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	admin := createUser(t, repo)
	target := createUser(t, repo)
	if err := repo.AssignRole(ctx, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignRole(ctx, target.ID, domain.RoleModerator); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateRefreshToken(ctx, domain.RefreshToken{ID: uuid.New(), UserID: target.ID, FamilyID: uuid.New(), TokenHash: uuid.NewString(), ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	granted, err := repo.Permissions(ctx, target.ID)
	if err != nil || !authz.NewSet(granted).Has(authz.ChatModerate) {
		t.Fatalf("moderator permissions = %v, %v", granted, err)
	}

	action := authz.Action{Service: "auth", Name: "user_suspended", ActorID: admin.ID, TargetType: "user", TargetID: target.ID.String(), At: now}
	if _, err := repo.SuspendUser(ctx, target.ID, "abuse", now, action); err != nil {
		t.Fatal(err)
	}
	user, err := repo.AdminUser(ctx, target.ID, now)
	if err != nil || user.ActiveSessions != 0 || user.User.SuspendedAt == nil {
		t.Fatalf("after suspend = %+v, %v", user, err)
	}
	granted, err = repo.Permissions(ctx, target.ID)
	if err != nil || len(granted) != 0 {
		t.Fatalf("suspended permissions = %v, %v", granted, err)
	}
	ok, err := repo.UserHasPermission(ctx, target.ID, "admin.chat", "read")
	if err != nil || ok {
		t.Fatalf("UserHasPermission for a suspended moderator = %v, %v", ok, err)
	}
	var outboxed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE aggregatetype = 'admin.audit.v1' AND aggregateid = $1`, target.ID.String()).Scan(&outboxed); err != nil || outboxed != 1 {
		t.Fatalf("admin.audit.v1 outbox rows = %d, %v", outboxed, err)
	}

	if _, err := repo.SuspendUser(ctx, target.ID, "again", now, action); !errors.Is(err, domain.ErrAlreadySuspended) {
		t.Fatalf("second suspend = %v", err)
	}
	if _, err := repo.RestoreUser(ctx, target.ID, now, action); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RestoreUser(ctx, target.ID, now, action); !errors.Is(err, domain.ErrNotSuspended) {
		t.Fatalf("second restore = %v", err)
	}
}

func TestConcurrentAdminRemovalKeepsOneAdmin(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()
	now := time.Now().UTC()
	first, second := createUser(t, repo), createUser(t, repo)
	for _, u := range []domain.User{first, second} {
		if err := repo.AssignRole(ctx, u.ID, domain.RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, pair := range [][2]domain.User{{first, second}, {second, first}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			action := authz.Action{Service: "auth", Name: "role_revoked", ActorID: pair[0].ID, At: now}
			errs[i] = repo.ChangeRole(ctx, pair[1].ID, domain.RoleAdmin, false, action)
		}()
	}
	wg.Wait()

	failures := 0
	for _, err := range errs {
		if errors.Is(err, domain.ErrLastAdmin) {
			failures++
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if failures != 1 {
		t.Fatalf("errors = %v, exactly one revoke must be refused", errs)
	}
	var admins int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE role_id = $1`, domain.RoleAdmin).Scan(&admins); err != nil || admins != 1 {
		t.Fatalf("admins left = %d, %v", admins, err)
	}
}

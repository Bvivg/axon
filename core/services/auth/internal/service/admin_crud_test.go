package service_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestCreateRoleNormalizesAndRecordsTheSnapshot(t *testing.T) {
	h := newHarness(t)
	admin, _ := adminAndTarget(t, h)
	id := uuid.New()

	tests := []struct {
		name    string
		dryRun  bool
		stored  bool
		changes int
	}{
		{name: "validate only", dryRun: true},
		{name: "create", stored: true, changes: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(h.store.recordedAdminActions())
			role, err := h.svc.CreateRole(t.Context(), admin, id, domain.AdminRole{Name: " ops ", Title: " Operations ", Permissions: []string{"rbac:read", "admin.users:read", "rbac:read"}}, "new team", tt.dryRun)
			if err != nil {
				t.Fatal(err)
			}
			if role.ID != id || role.Name != "ops" || role.Title != "Operations" || !slices.Equal(role.Permissions, []string{"admin.users:read", "rbac:read"}) {
				t.Fatalf("role = %+v", role)
			}
			if _, err := h.store.AdminRole(t.Context(), id); (err == nil) != tt.stored {
				t.Fatalf("stored = %v, want %v", err == nil, tt.stored)
			}
			actions := h.store.recordedAdminActions()[before:]
			if !tt.stored {
				if len(actions) != 0 {
					t.Fatalf("a dry run recorded %d actions", len(actions))
				}
				return
			}
			if len(actions) != 1 || actions[0].Name != "role_created" || actions[0].TargetType != "role" || len(actions[0].Changes) != tt.changes {
				t.Fatalf("actions = %+v", actions)
			}
		})
	}
}

func TestUpdateRoleChecksTheVersionAndRecordsTheDiff(t *testing.T) {
	h := newHarness(t)
	admin, _ := adminAndTarget(t, h)
	id := uuid.New()
	if _, err := h.svc.CreateRole(t.Context(), admin, id, domain.AdminRole{Name: "ops", Title: "Ops"}, "new team", false); err != nil {
		t.Fatal(err)
	}

	_, err := h.svc.UpdateRole(t.Context(), admin, id, 7, domain.RoleFields{Title: true}, domain.AdminRole{Title: "Operations"}, "rename", false)
	if conflict, ok := domain.AsVersionConflict(err); !ok || conflict.Current != 1 {
		t.Fatalf("stale version: %v", err)
	}

	role, err := h.svc.UpdateRole(t.Context(), admin, id, 1, domain.RoleFields{Title: true, Permissions: true}, domain.AdminRole{Title: "Operations", Permissions: []string{"rbac:read"}}, "rename", false)
	if err != nil {
		t.Fatal(err)
	}
	if role.Version != 2 || role.Title != "Operations" {
		t.Fatalf("role = %+v", role)
	}
	actions := h.store.recordedAdminActions()
	last := actions[len(actions)-1]
	if last.Name != "role_updated" || len(last.Changes) != 2 || last.Changes[0].Field != "title" || last.Changes[0].After != "Operations" {
		t.Fatalf("action = %+v", last)
	}
}

func TestSetUserRolesRefusesTheCallerAndRecordsTheChange(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)

	if _, err := h.svc.SetUserRoles(t.Context(), admin, admin, []string{"user"}, "demote myself"); !errors.Is(err, domain.ErrSelfAction) {
		t.Fatalf("self: %v", err)
	}
	user, err := h.svc.SetUserRoles(t.Context(), admin, target, []string{"moderator", "user", "moderator"}, "joins moderation")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(user.User.Roles, "moderator") {
		t.Fatalf("roles = %v", user.User.Roles)
	}
	if _, err := h.svc.SetUserRoles(t.Context(), admin, target, []string{"nobody"}, "typo"); !errors.Is(err, domain.ErrRoleNotFound) {
		t.Fatalf("unknown role: %v", err)
	}
}

func TestUpdateUserDisplayName(t *testing.T) {
	h := newHarness(t)
	admin, target := adminAndTarget(t, h)

	tests := []struct {
		name    string
		value   string
		version int64
		check   func(error) bool
	}{
		{name: "empty", value: "  ", version: 1, check: func(err error) bool { _, ok := domain.AsValidationError(err); return ok }},
		{name: "stale", value: "Nice name", version: 3, check: func(err error) bool { _, ok := domain.AsVersionConflict(err); return ok }},
		{name: "renamed", value: "Nice name", version: 1, check: func(err error) bool { return err == nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := h.svc.UpdateUserDisplayName(t.Context(), admin, target, tt.version, tt.value, "offensive name")
			if !tt.check(err) {
				t.Fatalf("err = %v", err)
			}
			if err == nil && user.User.DisplayName != "Nice name" {
				t.Fatalf("display name = %q", user.User.DisplayName)
			}
		})
	}
}

func TestExportAuditEventsTruncatesAndRecordsTheExport(t *testing.T) {
	h := newHarness(t)
	admin, _ := adminAndTarget(t, h)
	h.store.auditExport = make([]domain.AuditEvent, domain.MaxAuditExport+5)

	events, truncated, err := h.svc.ExportAuditEvents(t.Context(), admin, domain.AuditFilter{Service: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != domain.MaxAuditExport || !truncated {
		t.Fatalf("rows = %d, truncated = %v", len(events), truncated)
	}
	actions := h.store.recordedAdminActions()
	last := actions[len(actions)-1]
	if last.Name != "audit_exported" || last.Details["rows"] != "10000" || last.Details["truncated"] != "true" || last.Details["service"] != "chat" {
		t.Fatalf("action = %+v", last)
	}
}

package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type forbiddenPresence struct{ t *testing.T }

func (p forbiddenPresence) Online(context.Context, []string) (map[string]bool, error) {
	p.t.Fatal("authorization consulted optional presence")
	return nil, context.DeadlineExceeded
}

func TestCallSessionFamilyUsesDurableOwnershipRevocationAndExpiry(t *testing.T) {
	h := newHarness(t, func(cfg *service.Config) { cfg.Presence = forbiddenPresence{t} })
	registered := h.register(t, "calls-family@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	check := func(user, ref uuid.UUID, want bool) {
		t.Helper()
		active, err := h.svc.SessionFamilyActive(t.Context(), user, ref)
		if err != nil || active != want {
			t.Fatalf("active=%v want=%v error=%v", active, want, err)
		}
	}
	check(registered.User.ID, family, true)
	check(uuid.New(), family, false)
	check(registered.User.ID, uuid.New(), false)
	if err := h.svc.Logout(t.Context(), registered.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	check(registered.User.ID, family, false)
	fresh := h.register(t, "calls-expiry@example.com", validPassword)
	freshFamily := h.storedToken(t, fresh.Tokens.RefreshToken).FamilyID
	check(fresh.User.ID, freshFamily, true)
	h.advance(365 * 24 * time.Hour)
	check(fresh.User.ID, freshFamily, false)
}

func TestCallSessionListingCanSkipOptionalPresence(t *testing.T) {
	h := newHarness(t, func(cfg *service.Config) { cfg.Presence = forbiddenPresence{t} })
	registered := h.register(t, "calls-list@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	sessions, err := h.svc.ListSessionsWithPresence(t.Context(), registered.User.ID, family, false)
	if err != nil || len(sessions) != 1 || sessions[0].FamilyID != family || !sessions[0].Current {
		t.Fatalf("durable sessions=%v error=%v", sessions, err)
	}
}

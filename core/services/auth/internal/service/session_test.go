package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/shared/pkg/presence"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func withPresence(t *testing.T) (harnessOption, *presence.Tracker) {
	t.Helper()

	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	tracker := presence.NewTracker(client)

	return func(cfg *service.Config) {
		cfg.Presence = tracker
	}, tracker
}

func TestListSessionsReflectsLivePresence(t *testing.T) {
	opt, tracker := withPresence(t)
	h := newHarness(t, opt)
	registered := h.register(t, "ada@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID

	if err := tracker.Touch(context.Background(), family.String(), time.Minute); err != nil {
		t.Fatalf("touch: %v", err)
	}

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, family)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Online {
		t.Errorf("session online = %v, want true", sessions[0].Online)
	}
}

func TestListSessionsOfflineWhenPresenceExpired(t *testing.T) {
	opt, _ := withPresence(t)
	h := newHarness(t, opt)
	registered := h.register(t, "ada@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, family)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions[0].Online {
		t.Error("a session with no presence touch was marked online")
	}
}

func TestListSessionsWithoutPresenceTrackerDefaultsOffline(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, family)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if sessions[0].Online {
		t.Error("a session was marked online with no presence tracker configured")
	}
}

func TestListSessionsMarksTheCallersOwnSession(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)
	family := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, family)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if !sessions[0].Current {
		t.Error("the session matching the caller's own family_id was not marked current")
	}
}

func TestListSessionsOnlyOneFamilyIsCurrent(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	second, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "ada@example.com", Password: validPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	firstFamily := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	secondFamily := h.storedToken(t, second.Tokens.RefreshToken).FamilyID
	if firstFamily == secondFamily {
		t.Fatal("two sign-ins produced the same family_id")
	}

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, firstFamily)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	var currentCount int
	for _, s := range sessions {
		if s.Current {
			currentCount++
			if s.FamilyID != firstFamily {
				t.Errorf("marked %s current, want %s", s.FamilyID, firstFamily)
			}
		}
	}
	if currentCount != 1 {
		t.Errorf("%d sessions marked current, want exactly 1", currentCount)
	}
}

func TestRevokeSessionEndsOnlyThatSession(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	second, err := h.svc.Login(context.Background(), service.LoginInput{
		Email: "ada@example.com", Password: validPassword,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	firstFamily := h.storedToken(t, registered.Tokens.RefreshToken).FamilyID
	secondFamily := h.storedToken(t, second.Tokens.RefreshToken).FamilyID

	if err := h.svc.RevokeSession(context.Background(), registered.User.ID, secondFamily); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}

	sessions, err := h.svc.ListSessions(context.Background(), registered.User.ID, firstFamily)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions after revoke, want 1", len(sessions))
	}
	if sessions[0].FamilyID != firstFamily {
		t.Errorf("the surviving session is %s, want the untouched family %s", sessions[0].FamilyID, firstFamily)
	}

	if _, err := h.svc.Refresh(context.Background(), second.Tokens.RefreshToken, domain.Device{}); err == nil {
		t.Error("a token from the revoked family still refreshes")
	}
}

func TestRevokeSessionIsScopedToTheCaller(t *testing.T) {
	h := newHarness(t)
	victim := h.register(t, "ada@example.com", validPassword)
	attacker := h.register(t, "eve@example.com", validPassword)

	victimFamily := h.storedToken(t, victim.Tokens.RefreshToken).FamilyID

	err := h.svc.RevokeSession(context.Background(), attacker.User.ID, victimFamily)
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}

	sessions, err := h.svc.ListSessions(context.Background(), victim.User.ID, victimFamily)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("the victim's session was revoked by another user's request")
	}
}

func TestRevokeSessionUnknownID(t *testing.T) {
	h := newHarness(t)
	registered := h.register(t, "ada@example.com", validPassword)

	err := h.svc.RevokeSession(context.Background(), registered.User.ID, uuid.New())
	if !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

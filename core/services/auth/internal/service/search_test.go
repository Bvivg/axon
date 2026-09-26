package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestSearchUsersFindsAnExactEmailMatch(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "eve@example.com")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersFindsAnExactDisplayNameMatch(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	first, last := "Eve", "Online"
	if _, err := h.svc.UpdateProfile(context.Background(), target.User.ID, domain.ProfilePatch{
		FirstName: &first, LastName: &last,
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "Eve Online")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, strings.ToUpper(target.User.Email))
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersExcludesTheCaller(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)

	found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, caller.User.Email)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for a self-search", found)
	}
}

func TestSearchUsersNoMatchReturnsAnEmptyListNotAnError(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)

	found, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "nobody@example.com")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want empty", found)
	}
}

func TestSearchUsersRejectsABlankQuery(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)

	_, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "   ")
	if _, ok := domain.AsValidationError(err); !ok {
		t.Fatalf("err = %v, want a ValidationError", err)
	}
}

func TestUsersPublicProfilesSkipsUnknownIDs(t *testing.T) {
	h := newHarness(t)
	target := h.register(t, "ada@example.com", validPassword)

	found, err := h.svc.UsersPublicProfiles(context.Background(), []uuid.UUID{target.User.ID, uuid.New()})
	if err != nil {
		t.Fatalf("UsersPublicProfiles: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestUsersPublicProfilesWithAnEmptyIDListReturnsNoResults(t *testing.T) {
	h := newHarness(t)

	found, err := h.svc.UsersPublicProfiles(context.Background(), nil)
	if err != nil {
		t.Fatalf("UsersPublicProfiles: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want empty", found)
	}
}

func TestSearchUsersPropagatesAStoreFailure(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)

	boom := errors.New("boom")
	h.store.failOn["SearchExact"] = boom

	_, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "eve@example.com")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func TestSearchUsersDoesNotConfirmAnExactEmail(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	h.register(t, "eve@example.com", validPassword)

	for _, query := range []string{"eve@example.com", "EVE@EXAMPLE.COM"} {
		found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, query, 0, "")
		if err != nil {
			t.Fatalf("SearchUsers(%q): %v", query, err)
		}
		if len(found) != 0 {
			t.Fatalf("SearchUsers(%q) = %v, want no account confirmed by its address", query, found)
		}
	}
}

func TestSearchUsersNeedsAtLeastTwoCharacters(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	h.registerNamed(t, "eve@example.com", "Eve", "Online")

	for _, query := range []string{"", "   ", "e", "@e", " @ "} {
		_, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, query, 0, "")
		v, ok := domain.AsValidationError(err)
		if !ok || v.Field != "query" {
			t.Errorf("SearchUsers(%q) err = %v, want a validation error on query", query, err)
		}
	}

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "ev", 0, "")
	if err != nil || len(found) != 1 {
		t.Fatalf("SearchUsers(ev) = %v, %v; want the one match", found, err)
	}
}

func TestSearchResultsDoNotRevealLastSeen(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.registerNamed(t, "eve@example.com", "Eve", "Online")
	if err := h.svc.UpdateLastSeen(context.Background(), target.User.ID); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "eve", 0, "")
	if err != nil || len(found) != 1 {
		t.Fatalf("SearchUsers = %v, %v", found, err)
	}
	if found[0].User.LastSeenAt != nil {
		t.Error("a search result carries the person's last-seen time")
	}
}

func TestSearchUsersFindsAnExactDisplayNameMatch(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "eve@example.com", validPassword)

	first, last := "Eve", "Online"
	if _, err := h.svc.UpdateProfile(context.Background(), target.User.ID, uuid.Nil, domain.ProfilePatch{
		FirstName: &first, LastName: &last,
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "Eve Online", 0, "")
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
	target := h.registerNamed(t, "eve@example.com", "Eve", "Online")

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "EVE ONLINE", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersExcludesTheCaller(t *testing.T) {
	h := newHarness(t)
	caller := h.registerNamed(t, "ada@example.com", "Ada", "Lovelace")

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "Ada Lovelace", 0, "")
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

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "nobody@example.com", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want empty", found)
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
	h.store.failOn["SearchUsers"] = boom

	_, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "eve@example.com", 0, "")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func (h *harness) registerNamed(t *testing.T, email, first, last string) service.Result {
	t.Helper()

	res := h.register(t, email, validPassword)
	if _, err := h.svc.UpdateProfile(context.Background(), res.User.ID, uuid.Nil, domain.ProfilePatch{
		FirstName: &first, LastName: &last,
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	return res
}

func TestSearchUsersFindsAPartialNameMatch(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.registerNamed(t, "u1@example.com", "Eve", "Online")

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "ONL", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersFindsAPartialNicknameMatch(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	target := h.register(t, "u1@example.com", validPassword)

	if _, err := h.svc.UpdateProfile(context.Background(), target.User.ID, uuid.Nil, domain.ProfilePatch{
		Nickname: ptr("countess"),
	}); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "untes", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].User.ID != target.User.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.User.ID)
	}
}

func TestSearchUsersDoesNotMatchAPartialEmail(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	h.register(t, "eve@example.com", validPassword)

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "@example.com", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for an email fragment", found)
	}
}

func TestSearchUsersRanksExactThenPrefixThenSubstring(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	inside := h.registerNamed(t, "u1@example.com", "Steve", "Rogers")
	prefix := h.registerNamed(t, "u2@example.com", "Evelyn", "Grey")
	exact := h.registerNamed(t, "u3@example.com", "Eve", "")

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "eve", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	want := []uuid.UUID{exact.User.ID, prefix.User.ID, inside.User.ID}
	if len(found) != len(want) {
		t.Fatalf("found = %v, want %v", found, want)
	}
	for i, id := range want {
		if found[i].User.ID != id {
			t.Fatalf("found[%d] = %s, want %s", i, found[i].User.ID, id)
		}
	}
}

func TestSearchUsersCapsTheResultCount(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "ada@example.com", validPassword)
	for i := range 25 {
		h.registerNamed(t, fmt.Sprintf("clone%d@example.com", i), "Clone", fmt.Sprint(i))
	}

	found, _, err := h.svc.SearchUsers(context.Background(), caller.User.ID, "clone", 0, "")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 20 {
		t.Fatalf("len(found) = %d, want 20", len(found))
	}
}

func TestSearchUsersPagesThroughDirectoryAndMatches(t *testing.T) {
	for _, query := range []string{"pa", "page"} {
		t.Run(query, func(t *testing.T) {
			h := newHarness(t)
			caller := h.register(t, "caller@example.com", validPassword)
			for i := range 27 {
				h.registerNamed(t, fmt.Sprintf("page%d@example.com", i), "Page", fmt.Sprintf("%02d", i))
			}
			seen := map[uuid.UUID]bool{}
			var token string
			for page := range 4 {
				people, next, err := h.svc.SearchUsers(t.Context(), caller.User.ID, query, 7, token)
				if err != nil {
					t.Fatal(err)
				}
				want := 7
				if page == 3 {
					want = 6
				}
				if len(people) != want || (next == "") != (page == 3) {
					t.Fatalf("page %d: %d people, next=%q", page, len(people), next)
				}
				for _, person := range people {
					if seen[person.User.ID] || person.User.ID == caller.User.ID {
						t.Fatalf("duplicate or caller: %s", person.User.ID)
					}
					seen[person.User.ID] = true
				}
				token = next
			}
			if len(seen) != 27 {
				t.Fatalf("seen %d, want 27", len(seen))
			}
		})
	}
}

func TestSearchUsersRejectsInvalidPagination(t *testing.T) {
	h := newHarness(t)
	caller := h.register(t, "caller@example.com", validPassword)
	h.registerNamed(t, "one@example.com", "Page", "One")
	h.registerNamed(t, "two@example.com", "Page", "Two")
	_, token, err := h.svc.SearchUsers(t.Context(), caller.User.ID, "page", 1, "")
	if err != nil || token == "" {
		t.Fatalf("first page: %v, token=%q", err, token)
	}
	for _, tc := range []struct {
		query string
		size  int32
		token string
	}{
		{"page", -1, ""}, {"page", 101, ""}, {"page", 1, "broken"},
		{"another", 1, token}, {strings.Repeat("x", 255), 1, ""},
	} {
		_, _, err := h.svc.SearchUsers(t.Context(), caller.User.ID, tc.query, tc.size, tc.token)
		if _, ok := domain.AsValidationError(err); !ok {
			t.Fatalf("%+v: err=%v, want validation error", tc, err)
		}
	}
}

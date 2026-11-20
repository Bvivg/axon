//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const searchTestHash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"

func TestSearchUsersKeysetPagination(t *testing.T) {
	for _, query := range []string{"", "page"} {
		t.Run(query, func(t *testing.T) {
			repo := newRepo(t)
			caller := uuid.New()
			for i := range 23 {
				_, err := repo.CreateUserWithPassword(t.Context(), domain.User{
					ID: uuid.New(), Username: fmt.Sprintf("page_%02d", i),
					Email: fmt.Sprintf("page%d@axon.test", i), DisplayName: "Page Person",
				}, searchTestHash)
				if err != nil {
					t.Fatal(err)
				}
			}
			seen := map[uuid.UUID]bool{}
			var cursor *domain.UserSearchCursor
			for page := range 4 {
				users, err := repo.SearchUsers(t.Context(), query, caller, 7, cursor)
				if err != nil {
					t.Fatal(err)
				}
				want := 7
				if page == 3 {
					want = 2
				}
				if len(users) != want {
					t.Fatalf("page %d: got %d, want %d", page, len(users), want)
				}
				for _, user := range users {
					if seen[user.ID] {
						t.Fatalf("duplicate: %s", user.ID)
					}
					seen[user.ID] = true
				}
				last := users[len(users)-1]
				cursor = &last.Cursor
				if page == 0 {
					if _, err := pool.Exec(t.Context(), "DELETE FROM users WHERE id = $1", last.ID); err != nil {
						t.Fatal(err)
					}
					if _, err := repo.CreateUserWithPassword(t.Context(), domain.User{
						ID: uuid.New(), Username: "before_page", Email: "before@axon.test", DisplayName: "Page A",
					}, searchTestHash); err != nil {
						t.Fatal(err)
					}
				}
			}
			if len(seen) != 23 {
				t.Fatalf("seen %d, want 23", len(seen))
			}
		})
	}
}

func TestSearchUsersMatchesByEmail(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Username:    newUsername(),
		Email:       "search-target-" + uuid.NewString() + "@axon.test",
		DisplayName: "Ada Lovelace",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchUsers(ctx, target.Email, caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].ID != target.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.ID)
	}
}

func TestSearchUsersMatchesByDisplayNameCaseInsensitively(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Username:    newUsername(),
		Email:       "search-target-" + uuid.NewString() + "@axon.test",
		DisplayName: "Grace Hopper",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchUsers(ctx, strings.ToUpper(target.DisplayName), caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].ID != target.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.ID)
	}
}

func TestSearchUsersExcludesTheCaller(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-self-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchUsers(ctx, caller.Email, caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for a self-search", found)
	}
}

func TestSearchUsersDoesNotMatchAnEmailFragment(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	prefix := "search-prefix-" + uuid.NewString()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    prefix + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchUsers(ctx, prefix, caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for a bare prefix of %s", found, target.Email)
	}
}

func TestSearchUsersMatchesNameFragmentsRankedAndLiterally(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	tag := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	create := func(displayName string) domain.User {
		t.Helper()
		u, err := repo.CreateUserWithPassword(ctx, domain.User{
			ID:          uuid.New(),
			Username:    newUsername(),
			Email:       "search-rank-" + uuid.NewString() + "@axon.test",
			DisplayName: displayName,
		}, searchTestHash)
		if err != nil {
			t.Fatalf("create %q: %v", displayName, err)
		}
		return u
	}

	inside := create("Big " + tag + " Fan")
	prefix := create(tag + " Junior")
	exact := create(tag)
	create("Wild" + tag[:4] + "%" + tag[4:])
	caller := create("")

	found, err := repo.SearchUsers(ctx, strings.ToUpper(tag), caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	want := []uuid.UUID{exact.ID, prefix.ID, inside.ID}
	if len(found) != len(want) {
		t.Fatalf("found = %v, want %v", found, want)
	}
	for i, id := range want {
		if found[i].ID != id {
			t.Fatalf("found[%d] = %s (%q), want %s", i, found[i].ID, found[i].DisplayName, id)
		}
	}

	wildcard, err := repo.SearchUsers(ctx, tag[:4]+"%", caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers wildcard: %v", err)
	}
	if len(wildcard) != 1 || !strings.HasPrefix(wildcard[0].DisplayName, "Wild") {
		t.Fatalf("wildcard = %v, want only the name with a literal %%", wildcard)
	}

	capped, err := repo.SearchUsers(ctx, tag, caller.ID, 2, nil)
	if err != nil {
		t.Fatalf("SearchUsers capped: %v", err)
	}
	if len(capped) != 2 || capped[0].ID != exact.ID {
		t.Fatalf("capped = %v, want the 2 best matches", capped)
	}
}

func TestSearchUsersMatchesAUsernameFragment(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-uname-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchUsers(ctx, "@"+strings.ToUpper(target.Username[1:len(target.Username)-1]), caller.ID, 20, nil)
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(found) != 1 || found[0].ID != target.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.ID)
	}
}

func TestUsersByIDsReturnsOnlyKnownUsers(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	first, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-batch-1-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:       uuid.New(),
		Username: newUsername(),
		Email:    "search-batch-2-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	found, err := repo.UsersByIDs(ctx, []uuid.UUID{first.ID, second.ID, uuid.New()})
	if err != nil {
		t.Fatalf("UsersByIDs: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("found = %v, want exactly 2", found)
	}

	byID := map[uuid.UUID]bool{}
	for _, u := range found {
		byID[u.ID] = true
	}
	if !byID[first.ID] || !byID[second.ID] {
		t.Fatalf("found = %v, want %s and %s", found, first.ID, second.ID)
	}
}

func TestUsersByIDsWithAnEmptyListReturnsNoResults(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	found, err := repo.UsersByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("UsersByIDs: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want empty", found)
	}
}

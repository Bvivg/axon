//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

const searchTestHash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"

func TestSearchExactMatchesByEmail(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Email:       "search-target-" + uuid.NewString() + "@axon.test",
		DisplayName: "Ada Lovelace",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchExact(ctx, target.Email, caller.ID)
	if err != nil {
		t.Fatalf("SearchExact: %v", err)
	}
	if len(found) != 1 || found[0].ID != target.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.ID)
	}
}

func TestSearchExactMatchesByDisplayNameCaseInsensitively(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:          uuid.New(),
		Email:       "search-target-" + uuid.NewString() + "@axon.test",
		DisplayName: "Grace Hopper",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchExact(ctx, strings.ToUpper(target.DisplayName), caller.ID)
	if err != nil {
		t.Fatalf("SearchExact: %v", err)
	}
	if len(found) != 1 || found[0].ID != target.ID {
		t.Fatalf("found = %v, want exactly %s", found, target.ID)
	}
}

func TestSearchExactExcludesTheCaller(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-self-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchExact(ctx, caller.Email, caller.ID)
	if err != nil {
		t.Fatalf("SearchExact: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for a self-search", found)
	}
}

func TestSearchExactDoesNotPrefixMatch(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	prefix := "search-prefix-" + uuid.NewString()

	target, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: prefix + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	caller, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-caller-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create caller: %v", err)
	}

	found, err := repo.SearchExact(ctx, prefix, caller.ID)
	if err != nil {
		t.Fatalf("SearchExact: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found = %v, want no results for a bare prefix of %s", found, target.Email)
	}
}

func TestUsersByIDsReturnsOnlyKnownUsers(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	first, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-batch-1-" + uuid.NewString() + "@axon.test",
	}, searchTestHash)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: "search-batch-2-" + uuid.NewString() + "@axon.test",
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

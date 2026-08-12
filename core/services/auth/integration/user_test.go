//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

func TestRegistrationRollsBackOnADuplicateEmail(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	const hash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"

	first := createUser(t, repo)

	_, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: first.Email,
	}, hash)
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}

	if n := countUsers(t, first.Email); n != 1 {
		t.Errorf("%d users hold %q, want 1", n, first.Email)
	}
	if _, err := repo.CredentialByUserID(ctx, first.ID); err != nil {
		t.Errorf("the original credential was disturbed: %v", err)
	}
}

func TestEmailUniquenessIsEnforcedByTheDatabase(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	first := createUser(t, repo)

	_, err := repo.CreateUser(ctx, domain.User{ID: uuid.New(), Email: first.Email})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestOauthRegistrationLinksTheAccountAtomically(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	account := domain.OauthAccount{
		Provider:       domain.ProviderGoogle,
		ProviderUserID: "google-" + uuid.NewString(),
		Email:          "oauth-" + uuid.NewString() + "@axon.test",
	}

	created, err := repo.CreateUserWithOauthAccount(ctx, domain.User{
		ID:    uuid.New(),
		Email: account.Email,
	}, account)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	found, err := repo.OauthAccountByProviderID(ctx, account.Provider, account.ProviderUserID)
	if err != nil {
		t.Fatalf("the link was not created: %v", err)
	}
	if found.UserID != created.ID {
		t.Errorf("the link points at %s, want %s", found.UserID, created.ID)
	}
}

func TestAProviderIdentityNeverMovesToAnotherUser(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	providerUserID := "google-" + uuid.NewString()

	owner := createUser(t, repo)
	other := createUser(t, repo)

	link := domain.OauthAccount{
		UserID:         owner.ID,
		Provider:       domain.ProviderGoogle,
		ProviderUserID: providerUserID,
		Email:          owner.Email,
	}
	if err := repo.LinkOauthAccount(ctx, link); err != nil {
		t.Fatalf("first link: %v", err)
	}

	link.Email = "renamed-" + owner.Email
	if err := repo.LinkOauthAccount(ctx, link); err != nil {
		t.Fatalf("re-linking the same identity to the same user failed: %v", err)
	}
	if stored := mustLoadLink(t, repo, providerUserID); stored.Email != link.Email {
		t.Errorf("email = %q, want the provider's current one %q", stored.Email, link.Email)
	}

	link.UserID = other.ID
	link.Email = other.Email
	if err := repo.LinkOauthAccount(ctx, link); !errors.Is(err, domain.ErrOauthIdentityClaimed) {
		t.Fatalf("link by another user: err = %v, want ErrOauthIdentityClaimed", err)
	}

	stored := mustLoadLink(t, repo, providerUserID)
	if stored.UserID != owner.ID {
		t.Fatalf("the identity moved to %s; it must stay with %s", stored.UserID, owner.ID)
	}
	if stored.Email == other.Email {
		t.Error("another user's email was written onto someone else's provider link")
	}
}

func mustLoadLink(t *testing.T, repo *repository.Repository, providerUserID string) domain.OauthAccount {
	t.Helper()

	account, err := repo.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, providerUserID)
	if err != nil {
		t.Fatalf("load link %s: %v", providerUserID, err)
	}
	return account
}

func createUser(t *testing.T, repo *repository.Repository) domain.User {
	t.Helper()

	user, err := repo.CreateUserWithPassword(t.Context(), domain.User{
		ID:    uuid.New(),
		Email: "user-" + uuid.NewString() + "@axon.test",
	}, "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func countUsers(t *testing.T, email string) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

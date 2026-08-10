//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

// A user and its credential appear together or not at all. A failure between
// them would leave an account nobody can sign in to and nobody can register
// again either: the email is taken, but there is no password to check against.
//
// The rollback is the assertion. It is invisible from the service layer, which
// sees one call, and from e2e, which sees one error.
func TestRegistrationRollsBackOnADuplicateEmail(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	const hash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"

	first := createUser(t, repo)

	// The same address again. CreateUser fails inside the transaction, after
	// nothing and before the credential.
	_, err := repo.CreateUserWithPassword(ctx, domain.User{
		ID:    uuid.New(),
		Email: first.Email,
	}, hash)
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}

	// Exactly one user with that address, and it still has its credential.
	if n := countUsers(t, first.Email); n != 1 {
		t.Errorf("%d users hold %q, want 1", n, first.Email)
	}
	if _, err := repo.CredentialByUserID(ctx, first.ID); err != nil {
		t.Errorf("the original credential was disturbed: %v", err)
	}
}

// Email uniqueness is the database's job, not the service's. A check-then-insert
// in Go loses the race that this constraint cannot.
func TestEmailUniquenessIsEnforcedByTheDatabase(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	first := createUser(t, repo)

	_, err := repo.CreateUser(ctx, domain.User{ID: uuid.New(), Email: first.Email})
	if !errors.Is(err, domain.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

// An OAuth sign-in creates the user and the provider link together, for the
// mirror-image reason: a user without the link is unreachable, because the next
// sign-in from the same provider identity finds nothing and tries to register
// again against an address that is now taken.
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

// A provider identity stays with whoever claimed it first, and two different
// things follow from that.
//
// Signing in again is routine and must not error: the same user re-links on
// every sign-in, and the provider's current email is written through. Somebody
// else claiming the identity is refused — and the refusal is reported, which is
// the part that used to be missing. The conditional update matches no row, which
// is not a database error, so a caller that only checked err was told the link
// had been made and went on to act on it.
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

	// The same person signing in again: the email is refreshed from the
	// provider, and nothing else changes.
	link.Email = "renamed-" + owner.Email
	if err := repo.LinkOauthAccount(ctx, link); err != nil {
		t.Fatalf("re-linking the same identity to the same user failed: %v", err)
	}
	if stored := mustLoadLink(t, repo, providerUserID); stored.Email != link.Email {
		t.Errorf("email = %q, want the provider's current one %q", stored.Email, link.Email)
	}

	// Someone else claiming it: the ON CONFLICT update is skipped, no row is
	// touched, and that has to reach the caller as a refusal rather than as
	// silence. What matters is both the error and the row afterwards.
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

// createUser makes an account with a unique address and a password.
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

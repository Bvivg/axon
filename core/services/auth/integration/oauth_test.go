//go:build integration

package integration

import (
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestOauthAccountHasNoCredential(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	account := domain.OauthAccount{
		Provider:       domain.ProviderGoogle,
		ProviderUserID: "google-" + uuid.NewString(),
		Email:          "provider-only-" + uuid.NewString() + "@axon.test",
	}

	created, err := repo.CreateUserWithOauthAccount(ctx, domain.User{
		ID:            uuid.New(),
		Email:         account.Email,
		EmailVerified: true,
	}, account)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = repo.CredentialByUserID(ctx, created.ID)
	if !errors.Is(err, domain.ErrNoPassword) {
		t.Fatalf("err = %v, want ErrNoPassword", err)
	}
}

func TestLinkingAProviderLeavesThePasswordIntact(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	before, err := repo.CredentialByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("load credential: %v", err)
	}

	err = repo.LinkOauthAccount(ctx, domain.OauthAccount{
		UserID:         user.ID,
		Provider:       domain.ProviderGitHub,
		ProviderUserID: "github-" + uuid.NewString(),
		Email:          user.Email,
	})
	if err != nil {
		t.Fatalf("link: %v", err)
	}

	after, err := repo.CredentialByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("load credential after linking: %v", err)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Error("linking a provider changed the stored password")
	}
}

func TestSeveralProvidersCanPointAtOneAccount(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	user := createUser(t, repo)

	links := map[domain.Provider]string{
		domain.ProviderGoogle: "google-" + uuid.NewString(),
		domain.ProviderGitHub: "github-" + uuid.NewString(),
	}

	for provider, subject := range links {
		err := repo.LinkOauthAccount(ctx, domain.OauthAccount{
			UserID:         user.ID,
			Provider:       provider,
			ProviderUserID: subject,
			Email:          user.Email,
		})
		if err != nil {
			t.Fatalf("link %s: %v", provider, err)
		}
	}

	for provider, subject := range links {
		found, err := repo.OauthAccountByProviderID(ctx, provider, subject)
		if err != nil {
			t.Fatalf("look up %s: %v", provider, err)
		}
		if found.UserID != user.ID {
			t.Errorf("%s resolves to %s, want %s", provider, found.UserID, user.ID)
		}
	}

	linked, err := repo.OauthAccountsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(linked) != len(links) {
		t.Errorf("the account lists %d providers, want %d", len(linked), len(links))
	}
}

func TestCreatingAnAccountForATakenIdentityLeavesNothingBehind(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	owner := createUser(t, repo)

	subject := "github-" + uuid.NewString()
	if err := repo.LinkOauthAccount(ctx, domain.OauthAccount{
		Provider:       domain.ProviderGitHub,
		ProviderUserID: subject,
		UserID:         owner.ID,
		Email:          owner.Email,
	}); err != nil {
		t.Fatalf("link the identity to its owner: %v", err)
	}

	email := "newcomer-" + uuid.NewString() + "@axon.test"

	_, err := repo.CreateUserWithOauthAccount(ctx, domain.User{
		ID:            uuid.New(),
		Email:         email,
		EmailVerified: true,
	}, domain.OauthAccount{
		Provider:       domain.ProviderGitHub,
		ProviderUserID: subject,
		Email:          email,
	})
	if !errors.Is(err, domain.ErrOauthIdentityClaimed) {
		t.Fatalf("err = %v, want ErrOauthIdentityClaimed", err)
	}

	if n := countUsers(t, email); n != 0 {
		t.Errorf("%d users were left behind by the rolled-back creation, want 0", n)
	}
}

func TestTheSameSubjectAtTwoProvidersIsTwoIdentities(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	subject := "shared-subject-" + uuid.NewString()

	first := createUser(t, repo)
	second := createUser(t, repo)

	for user, provider := range map[uuid.UUID]domain.Provider{
		first.ID:  domain.ProviderGoogle,
		second.ID: domain.ProviderGitHub,
	} {
		err := repo.LinkOauthAccount(ctx, domain.OauthAccount{
			UserID:         user,
			Provider:       provider,
			ProviderUserID: subject,
			Email:          "x@axon.test",
		})
		if err != nil {
			t.Fatalf("link %s: %v", provider, err)
		}
	}

	google, err := repo.OauthAccountByProviderID(ctx, domain.ProviderGoogle, subject)
	if err != nil {
		t.Fatalf("look up google: %v", err)
	}
	github, err := repo.OauthAccountByProviderID(ctx, domain.ProviderGitHub, subject)
	if err != nil {
		t.Fatalf("look up github: %v", err)
	}

	if google.UserID == github.UserID {
		t.Error("the same subject at two providers resolved to one account")
	}
}

func TestConcurrentFirstSignInsProduceOneAccount(t *testing.T) {
	repo := newRepo(t)
	ctx := t.Context()

	subject := "google-" + uuid.NewString()
	email := "racer-" + uuid.NewString() + "@axon.test"

	const racers = 4

	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		created int
	)

	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			<-start
			_, err := repo.CreateUserWithOauthAccount(ctx, domain.User{
				ID:            uuid.New(),
				Email:         email,
				EmailVerified: true,
			}, domain.OauthAccount{
				Provider:       domain.ProviderGoogle,
				ProviderUserID: subject,
				Email:          email,
			})

			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				created++
			}
		}()
	}

	close(start)
	wg.Wait()

	if created != 1 {
		t.Errorf("%d of %d concurrent first sign-ins created an account; exactly one must", created, racers)
	}
	if n := countUsers(t, email); n != 1 {
		t.Errorf("%d users hold %q, want 1", n, email)
	}
}

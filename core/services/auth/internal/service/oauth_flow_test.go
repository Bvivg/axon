package service_test

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

func TestOauthSignInCreatesAnAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	result := h.signIn(t, identity("subject-1", "newcomer@example.test"))

	if !result.Created {
		t.Error("a first-time sign-in did not report the account as created")
	}
	if result.User.Email != "newcomer@example.test" {
		t.Errorf("email = %q", result.User.Email)
	}
	if result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" {
		t.Error("a successful sign-in returned no tokens")
	}

	if !result.User.EmailVerified {
		t.Error("an address verified by the provider was not recorded as verified")
	}
}

func TestTheClientsNameIsUsedOnlyWhenTheProviderGaveNone(t *testing.T) {
	for name, tc := range map[string]struct {
		fromProvider string
		fromClient   string
		want         string
	}{
		"only the client has one":     {fromClient: "Ada Lovelace", want: "Ada Lovelace"},
		"the provider's name wins":    {fromProvider: "Ada L.", fromClient: "Someone Else", want: "Ada L."},
		"neither has one":             {},
		"the client sends whitespace": {fromClient: "   ", want: ""},

		"only the provider has one": {fromProvider: "Ada Lovelace", want: "Ada Lovelace"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, withOAuth(t))

			who := identity("subject-1", "person@example.test")
			if tc.fromProvider != "" {
				who.Set("name", tc.fromProvider)
			}

			result, err := h.trySignInAs(t, who, tc.fromClient)
			if err != nil {
				t.Fatalf("sign in: %v", err)
			}
			if result.User.DisplayName != tc.want {
				t.Errorf("display name = %q, want %q", result.User.DisplayName, tc.want)
			}
		})
	}
}

func TestAnUnusableNameFromTheClientDoesNotFailTheSignIn(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	tooLong := strings.Repeat("n", domain.MaxDisplayNameLength+1)

	result, err := h.trySignInAs(t, identity("subject-1", "person@example.test"), tooLong)
	if err != nil {
		t.Fatalf("an over-long name failed the sign-in: %v", err)
	}
	if result.User.DisplayName != "" {
		t.Errorf("display name = %q, want it dropped", result.User.DisplayName)
	}
}

func TestTheClientsNameDoesNotRenameAnExistingAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	registered, err := h.svc.Register(t.Context(), service.RegisterInput{
		Email:       "person@example.test",
		Password:    validPassword,
		DisplayName: "Ada Lovelace",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	result, err := h.trySignInAs(t, identity("subject-1", "person@example.test"), "Someone Else")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	if result.User.ID != registered.User.ID {
		t.Fatal("the sign-in did not resolve to the account it matched by address")
	}
	if result.User.DisplayName != "Ada Lovelace" {
		t.Errorf("display name = %q, want the account's own", result.User.DisplayName)
	}
}

func TestReturningUserResolvesToTheSameAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	first := h.signIn(t, identity("subject-1", "person@example.test"))

	second := h.signIn(t, url.Values{
		"sub":   {"subject-1"},
		"email": {"renamed@example.test"},
	})

	if second.Created {
		t.Error("a returning user was reported as newly created")
	}
	if second.User.ID != first.User.ID {
		t.Errorf("the same provider identity produced two accounts: %s and %s",
			first.User.ID, second.User.ID)
	}
}

func TestProviderSignInLinksToAnExistingPasswordAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const email = "both-ways@example.test"
	existing := h.register(t, email, validPassword)

	result := h.signIn(t, identity("subject-1", email))

	if result.Created {
		t.Error("linking to an existing account was reported as a creation")
	}
	if result.User.ID != existing.User.ID {
		t.Errorf("a second account was created for %s", email)
	}

	if _, err := h.svc.Login(t.Context(), service.LoginInput{
		Email:    email,
		Password: validPassword,
	}); err != nil {
		t.Errorf("the password stopped working after linking a provider: %v", err)
	}
}

func TestAnUnverifiedAddressCannotClaimAnAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const email = "victim@example.test"
	h.register(t, email, validPassword)

	_, err := h.trySignIn(t, url.Values{
		"sub":            {"attacker-subject"},
		"email":          {email},
		"email_verified": {"false"},
	})
	if !errors.Is(err, domain.ErrOauthEmailUnverified) {
		t.Fatalf("err = %v, want ErrOauthEmailUnverified", err)
	}

	if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderFake, "attacker-subject"); err == nil {
		t.Error("a link was created despite the sign-in being refused")
	}
	if _, err := h.svc.Login(t.Context(), service.LoginInput{
		Email:    email,
		Password: validPassword,
	}); err != nil {
		t.Errorf("the victim's own account was disturbed: %v", err)
	}
}

func TestASignInIsRefusedWhenTheIdentityIsClaimedMidFlow(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const (
		email    = "matched-by-address@example.test"
		claimant = "someone-else@example.test"
		subject  = "contested-subject"
	)

	matched := h.register(t, email, validPassword)
	other := h.register(t, claimant, validPassword)

	h.store.beforeLink = func() {
		h.store.beforeLink = nil
		h.store.linkDirectly(domain.OauthAccount{
			Provider:       domain.ProviderFake,
			ProviderUserID: subject,
			UserID:         other.User.ID,
			Email:          claimant,
		})
	}

	_, err := h.trySignIn(t, identity(subject, email))
	if !errors.Is(err, domain.ErrOauthIdentityClaimed) {
		t.Fatalf("err = %v, want ErrOauthIdentityClaimed", err)
	}

	link, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderFake, subject)
	if err != nil {
		t.Fatalf("look up the contested identity: %v", err)
	}
	if link.UserID != other.User.ID {
		t.Errorf("the identity moved to %s, want %s", link.UserID, other.User.ID)
	}

	if n := h.store.liveTokensForUser(matched.User.ID, h.clock); n != 1 {
		t.Errorf("the address-matched account holds %d live tokens, want only the one from registration", n)
	}
	if _, err := h.svc.Login(t.Context(), service.LoginInput{Email: email, Password: validPassword}); err != nil {
		t.Errorf("the address-matched account was disturbed: %v", err)
	}
}

func TestStateCannotBeReplayed(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	if _, err := h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderFake,
		Code:     code,
		State:    started.State,
	}); err != nil {
		t.Fatalf("the first completion failed: %v", err)
	}

	_, err = h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderFake,
		Code:     code,
		State:    started.State,
	})
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

func TestStateIsBoundToItsProvider(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	_, err = h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderGoogle,
		Code:     code,
		State:    started.State,
	})
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

func TestAnUnknownStateIsRefused(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	_, err := h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderFake,
		Code:     "some-code",
		State:    "never-issued",
	})
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

func TestStartOauthChecksTheDestination(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	if _, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "https://evil.example/collect"); !errors.Is(err, domain.ErrOauthReturnToNotAllowed) {
		t.Fatalf("err = %v, want ErrOauthReturnToNotAllowed", err)
	}

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, webOrigin+"/lobby")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	completed, err := h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderFake,
		Code:     code,
		State:    started.State,
	})
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}
	if completed.ReturnTo != webOrigin+"/lobby" {
		t.Errorf("return_to = %q, want the destination agreed at the start", completed.ReturnTo)
	}
}

func TestUnconfiguredProvidersAreRefused(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	for _, provider := range []domain.Provider{domain.ProviderApple, domain.ProviderGitHub} {
		if _, err := h.svc.StartOAuth(t.Context(), provider, ""); !errors.Is(err, domain.ErrProviderUnsupported) {
			t.Errorf("StartOAuth(%q) = %v, want ErrProviderUnsupported", provider, err)
		}
	}
}

func TestOauthIsUnsupportedWhenNothingIsConfigured(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.StartOAuth(t.Context(), domain.ProviderGoogle, ""); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("StartOAuth = %v, want ErrProviderUnsupported", err)
	}
	completeGoogle := service.CompleteOAuthInput{Provider: domain.ProviderGoogle, Code: "c", State: "s"}
	if _, err := h.svc.CompleteOAuth(t.Context(), completeGoogle); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("CompleteOAuth = %v, want ErrProviderUnsupported", err)
	}
}

func TestEachOauthSignInStartsItsOwnRefreshFamily(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	first := h.signIn(t, identity("subject-1", "person@example.test"))
	second := h.signIn(t, identity("subject-1", "person@example.test"))

	firstFamily := h.storedToken(t, first.Tokens.RefreshToken).FamilyID
	secondFamily := h.storedToken(t, second.Tokens.RefreshToken).FamilyID

	if firstFamily == secondFamily {
		t.Error("two sign-ins share one refresh family")
	}
}

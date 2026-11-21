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

	who := identity("github-1", "person@example.test")
	who.Set("name", "Ada Lovelace")
	created, err := h.trySignInWith(t, domain.ProviderGitHub, who, "")
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}

	result, err := h.trySignInAs(t, identity("subject-1", "person@example.test"), "Someone Else")
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	if result.User.ID != created.User.ID {
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

func TestProviderSignInLinksToAnAccountWithAVerifiedEmail(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const email = "both-ways@example.test"
	existing, err := h.trySignInWith(t, domain.ProviderGitHub, identity("github-1", email), "")
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if !existing.User.EmailVerified {
		t.Fatal("a provider-created account was not recorded as verified")
	}

	result := h.signIn(t, identity("subject-1", email))

	if result.Created {
		t.Error("linking to an existing account was reported as a creation")
	}
	if result.User.ID != existing.User.ID {
		t.Errorf("a second account was created for %s", email)
	}
}

func TestProviderSignInDoesNotLinkToAnUnverifiedAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const email = "victim@example.test"
	squatter := h.register(t, email, validPassword)

	_, err := h.trySignIn(t, identity("victim-google", email))
	if !errors.Is(err, domain.ErrOauthAccountExists) {
		t.Fatalf("err = %v, want ErrOauthAccountExists", err)
	}

	if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, "victim-google"); err == nil {
		t.Error("the provider identity was linked to an account nobody verified")
	}
	if n := h.store.liveTokensForUser(squatter.User.ID, h.clock); n != 1 {
		t.Errorf("the unverified account holds %d live tokens, want only the one from registration", n)
	}
}

func TestAChangedEmailCannotBeClaimedByAProvider(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	victim := h.signIn(t, identity("victim-google", "victim@example.test"))
	family := h.storedToken(t, victim.Tokens.RefreshToken).FamilyID

	changed, err := h.svc.UpdateProfile(t.Context(), victim.User.ID, family, domain.ProfilePatch{Email: ptr("attacker@example.test")})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if changed.EmailVerified {
		t.Fatal("the changed address is still marked verified")
	}

	_, err = h.trySignIn(t, identity("attacker-google", "attacker@example.test"))
	if !errors.Is(err, domain.ErrOauthAccountExists) {
		t.Fatalf("err = %v, want ErrOauthAccountExists", err)
	}
	if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, "attacker-google"); err == nil {
		t.Error("the attacker's identity was linked to the victim's account")
	}
}

func TestTheFakeProviderOnlySignsInItsReservedDomain(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	h.register(t, "victim@example.test", validPassword)

	_, err := h.trySignInFake(t, identity("attacker", "victim@example.test"))
	if !errors.Is(err, domain.ErrOauthEmailNotAllowed) {
		t.Fatalf("err = %v, want ErrOauthEmailNotAllowed", err)
	}

	created, err := h.trySignInFake(t, identity("tester", "tester@fake.axon.test"))
	if err != nil {
		t.Fatalf("sign in with a reserved address: %v", err)
	}
	if !created.Created || created.User.Email != "tester@fake.axon.test" {
		t.Errorf("result = %+v, want a new account for the reserved address", created.User)
	}
}

func TestTheFakeProviderNeverLinksToAnExistingAccount(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	password := h.register(t, "taken@fake.axon.test", validPassword)
	verified, err := h.trySignInWith(t, domain.ProviderGitHub, identity("github-1", "verified@fake.axon.test"), "")
	if err != nil {
		t.Fatalf("GitHub sign-in: %v", err)
	}

	for _, target := range []domain.User{password.User, verified.User} {
		_, err := h.trySignInFake(t, identity("attacker-"+target.Email, target.Email))
		if !errors.Is(err, domain.ErrOauthAccountExists) {
			t.Fatalf("fake sign-in over %s: err = %v, want ErrOauthAccountExists", target.Email, err)
		}
		if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderFake, "attacker-"+target.Email); err == nil {
			t.Errorf("the fake identity was linked to %s", target.Email)
		}
	}

	if _, err := h.svc.Login(t.Context(), service.LoginInput{Email: "taken@fake.axon.test", Password: validPassword}); err != nil {
		t.Errorf("the password account was disturbed: %v", err)
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

	if _, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, "attacker-subject"); err == nil {
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

	matched, err := h.trySignInWith(t, domain.ProviderGitHub, identity("github-matched", email), "")
	if err != nil {
		t.Fatalf("GitHub sign-in: %v", err)
	}
	other := h.register(t, claimant, validPassword)

	h.store.beforeLink = func() {
		h.store.beforeLink = nil
		h.store.linkDirectly(domain.OauthAccount{
			Provider:       domain.ProviderGoogle,
			ProviderUserID: subject,
			UserID:         other.User.ID,
			Email:          claimant,
		})
	}

	_, err = h.trySignIn(t, identity(subject, email))
	if !errors.Is(err, domain.ErrOauthIdentityClaimed) {
		t.Fatalf("err = %v, want ErrOauthIdentityClaimed", err)
	}

	link, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderGoogle, subject)
	if err != nil {
		t.Fatalf("look up the contested identity: %v", err)
	}
	if link.UserID != other.User.ID {
		t.Errorf("the identity moved to %s, want %s", link.UserID, other.User.ID)
	}

	if n := h.store.liveTokensForUser(matched.User.ID, h.clock); n != 1 {
		t.Errorf("the address-matched account holds %d live tokens, want only the one from its first sign-in", n)
	}
}

func TestStateCannotBeReplayed(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@fake.axon.test"))

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

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@fake.axon.test"))

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

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@fake.axon.test"))

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

	for _, provider := range []domain.Provider{domain.ProviderApple} {
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

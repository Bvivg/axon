package service_test

import (
	"errors"
	"net/url"
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

	// The provider vouched for the address, so asking the person to prove it
	// again would be asking them to repeat something already done.
	if !result.User.EmailVerified {
		t.Error("an address verified by the provider was not recorded as verified")
	}
}

// The same person coming back is one account, not two. The match is on the
// provider's subject identifier, so it holds even if they change their address
// at the provider.
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

// Someone who registered with a password and later signs in with a provider is
// the same person, and gets the same account rather than a duplicate.
//
// This branch is only safe because an unverified address never reaches it. That
// is what the next test is about.
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

	// The password still works: linking a provider adds a way in, it does not
	// take one away.
	if _, err := h.svc.Login(t.Context(), service.LoginInput{
		Email:    email,
		Password: validPassword,
	}); err != nil {
		t.Errorf("the password stopped working after linking a provider: %v", err)
	}
}

// The account takeover this refusal exists to prevent: register victim@ at a
// provider that never confirms addresses, sign in here, inherit their account.
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

	// And nothing was linked on the way to failing.
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

// Linking to an account matched by address is safe only while the identity is
// still unclaimed. It can stop being unclaimed between the lookup that found
// nothing and the write that links it — two devices signing in at once, or
// someone racing the flow on purpose.
//
// The store refuses to move the identity either way. What is under test here is
// that the refusal ends the sign-in: letting the person into the address-matched
// account regardless would seat them in an account the identity does not point
// at, and the next sign-in would resolve the same identity to the other one.
func TestASignInIsRefusedWhenTheIdentityIsClaimedMidFlow(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	const (
		email    = "matched-by-address@example.test"
		claimant = "someone-else@example.test"
		subject  = "contested-subject"
	)

	matched := h.register(t, email, validPassword)
	other := h.register(t, claimant, validPassword)

	// Planted after resolveOauthUser has looked and found nothing, which is
	// exactly the window the store's conditional update closes.
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

	// The identity stayed with whoever claimed it first.
	link, err := h.store.OauthAccountByProviderID(t.Context(), domain.ProviderFake, subject)
	if err != nil {
		t.Fatalf("look up the contested identity: %v", err)
	}
	if link.UserID != other.User.ID {
		t.Errorf("the identity moved to %s, want %s", link.UserID, other.User.ID)
	}

	// And the account that merely shared an address is untouched: no session
	// was issued for it, and its password still works.
	if n := h.store.liveTokensForUser(matched.User.ID, h.clock); n != 1 {
		t.Errorf("the address-matched account holds %d live tokens, want only the one from registration", n)
	}
	if _, err := h.svc.Login(t.Context(), service.LoginInput{Email: email, Password: validPassword}); err != nil {
		t.Errorf("the address-matched account was disturbed: %v", err)
	}
}

// A state may be spent once. A second callback carrying it is a replay.
func TestStateCannotBeReplayed(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	if _, err := h.svc.CompleteOAuth(t.Context(), domain.ProviderFake, code, started.State); err != nil {
		t.Fatalf("the first completion failed: %v", err)
	}

	_, err = h.svc.CompleteOAuth(t.Context(), domain.ProviderFake, code, started.State)
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

// A state minted for one provider must not close a flow at another: otherwise
// someone holding a state for a provider they control can spend it against one
// they do not.
func TestStateIsBoundToItsProvider(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	_, err = h.svc.CompleteOAuth(t.Context(), domain.ProviderGoogle, code, started.State)
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

func TestAnUnknownStateIsRefused(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	_, err := h.svc.CompleteOAuth(t.Context(), domain.ProviderFake, "some-code", "never-issued")
	if !errors.Is(err, domain.ErrOauthStateInvalid) {
		t.Fatalf("err = %v, want ErrOauthStateInvalid", err)
	}
}

func TestStartOauthChecksTheDestination(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	if _, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "https://evil.example/collect"); !errors.Is(err, domain.ErrOauthReturnToNotAllowed) {
		t.Fatalf("err = %v, want ErrOauthReturnToNotAllowed", err)
	}

	// Checked when the flow starts, so the callback uses what was agreed rather
	// than anything it is handed.
	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, webOrigin+"/lobby")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code := h.follow(t, started.AuthorizationURL, identity("subject-1", "person@example.test"))

	completed, err := h.svc.CompleteOAuth(t.Context(), domain.ProviderFake, code, started.State)
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}
	if completed.ReturnTo != webOrigin+"/lobby" {
		t.Errorf("return_to = %q, want the destination agreed at the start", completed.ReturnTo)
	}
}

func TestUnconfiguredProvidersAreRefused(t *testing.T) {
	h := newHarness(t, withOAuth(t))

	// Apple is not implemented yet; GitHub has no credentials in this harness.
	for _, provider := range []domain.Provider{domain.ProviderApple, domain.ProviderGitHub} {
		if _, err := h.svc.StartOAuth(t.Context(), provider, ""); !errors.Is(err, domain.ErrProviderUnsupported) {
			t.Errorf("StartOAuth(%q) = %v, want ErrProviderUnsupported", provider, err)
		}
	}
}

// A deployment that configured no provider still serves the password flow. The
// OAuth procedures have to say so rather than panic on a nil dependency.
func TestOauthIsUnsupportedWhenNothingIsConfigured(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.StartOAuth(t.Context(), domain.ProviderGoogle, ""); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("StartOAuth = %v, want ErrProviderUnsupported", err)
	}
	if _, err := h.svc.CompleteOAuth(t.Context(), domain.ProviderGoogle, "c", "s"); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("CompleteOAuth = %v, want ErrProviderUnsupported", err)
	}
}

// Two sign-ins are two devices, so each starts its own refresh chain: revoking
// one because its token was replayed must not sign the other out.
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

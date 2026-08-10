//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
)

// t0 is the context these calls run under. The helpers below are called from
// both a test body and its helpers, so they take no testing.T of their own.
func t0() context.Context { return context.Background() }

// startOAuth begins a provider sign-in through the gateway.
func (c *caller) startOAuth(provider authv1.OauthProvider, returnTo string) (*authv1.StartOAuthResponse, error) {
	req := &authv1.StartOAuthRequest{Provider: provider}
	if returnTo != "" {
		req.ReturnTo = &returnTo
	}

	resp, err := c.client.StartOAuth(t0(), connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// completeOAuth finishes one. displayName stands for the name a provider hands
// the client instead of the service — Apple, in practice — and is left out of
// the request when empty, as it is for every other provider.
func (c *caller) completeOAuth(
	provider authv1.OauthProvider,
	code, state, displayName string,
) (*authv1.CompleteOAuthResponse, error) {
	req := &authv1.CompleteOAuthRequest{
		Provider: provider,
		Code:     code,
		State:    state,
	}
	if displayName != "" {
		req.DisplayName = &displayName
	}

	resp, err := c.client.CompleteOAuth(t0(), connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// consent plays the browser's part: follow the authorization URL, stop at the
// redirect, and read the code out of it.
//
// Nothing here pokes at the service's internals. This is the same sequence a
// browser performs, over the same HTTP, which is why the fake provider is worth
// more than a mock would be.
func consent(t *testing.T, authorizationURL string, extra url.Values) (code, state string) {
	t.Helper()

	target := authorizationURL
	if len(extra) > 0 {
		target += "&" + extra.Encode()
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build the authorization request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("follow the authorization URL: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the provider answered %d, want a redirect", resp.StatusCode)
	}

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse the callback URL: %v", err)
	}

	query := location.Query()
	return query.Get("code"), query.Get("state")
}

// signInWith walks a whole provider sign-in and returns the result.
func (c *caller) signInWith(t *testing.T, extra url.Values) (*authv1.CompleteOAuthResponse, error) {
	t.Helper()

	return c.signInAs(t, extra, "")
}

// signInAs is signInWith, with the client supplying a name of its own.
func (c *caller) signInAs(
	t *testing.T,
	extra url.Values,
	displayName string,
) (*authv1.CompleteOAuthResponse, error) {
	t.Helper()

	started, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code, returned := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()), extra)

	// The provider hands the state back untouched. A client that could not match
	// the callback to the flow it started would have no way to tell a genuine
	// callback from an injected one.
	if returned != started.GetState() {
		t.Fatalf("state came back as %q, want %q", returned, started.GetState())
	}

	return c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), displayName)
}

// rewriteForRunner points the authorization URL at the address the runner can
// reach.
//
// The service builds it from OAUTH_FAKE_AUTHORIZE_URL, which is what a browser
// would use. Inside the compose network those are the same host, so this is
// normally the identity function — it exists so the suite still works when the
// two differ.
func rewriteForRunner(t *testing.T, authorizationURL string) string {
	t.Helper()

	base := os.Getenv("OAUTH_FAKE_AUTHORIZE_URL")
	if base == "" {
		return authorizationURL
	}

	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("parse the authorization URL: %v", err)
	}

	reachable, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse OAUTH_FAKE_AUTHORIZE_URL: %v", err)
	}

	parsed.Scheme, parsed.Host = reachable.Scheme, reachable.Host
	return parsed.String()
}

func fakeIdentity(subject, email string) url.Values {
	return url.Values{"sub": {subject}, "email": {email}}
}

// The whole flow, end to end, through the gateway: start, consent, complete,
// and then use what came back on a protected procedure.
func TestOAuthSignInCreatesAnAccount(t *testing.T) {
	c := newCaller(t)

	subject := "e2e-" + uuid.NewString()
	address := subject + "@axon.test"

	result, err := c.signInWith(t, fakeIdentity(subject, address))
	if err != nil {
		t.Fatalf("sign in with the fake provider: %v", err)
	}

	if !result.GetCreated() {
		t.Error("a first-time sign-in did not report the account as created")
	}
	if got := result.GetUser().GetEmail(); got != address {
		t.Errorf("email = %q, want %q", got, address)
	}

	// The tokens are the same kind the password flow issues, which is the point
	// of routing both through one issuer.
	user, err := c.getMe(result.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with an OAuth token: %v", err)
	}
	if user.GetId() != result.GetUser().GetId() {
		t.Errorf("GetMe returned a different user than the sign-in did")
	}
}

// Apple sends the name to the client and never to this service, so the client
// passes it back on the request. This is the path that field travels: through
// the gateway, through the exchange, onto the account it creates — and back out
// of GetMe, which is where a client would read it.
func TestTheClientsNameReachesTheAccountItCreates(t *testing.T) {
	c := newCaller(t)

	subject := "e2e-" + uuid.NewString()
	address := subject + "@axon.test"
	const name = "Ada Lovelace"

	// No name in the identity: the fake provider offers one only when asked, so
	// this is the gap Apple leaves.
	result, err := c.signInAs(t, fakeIdentity(subject, address), name)
	if err != nil {
		t.Fatalf("sign in with the fake provider: %v", err)
	}
	if got := result.GetUser().GetDisplayName(); got != name {
		t.Errorf("display name = %q, want %q", got, name)
	}

	user, err := c.getMe(result.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if got := user.GetDisplayName(); got != name {
		t.Errorf("GetMe display name = %q, want it persisted as %q", got, name)
	}
}

// The same provider identity is one account however many times it signs in, and
// the match survives the person changing their address at the provider.
func TestReturningOAuthUserGetsTheSameAccount(t *testing.T) {
	c := newCaller(t)

	subject := "e2e-" + uuid.NewString()
	address := subject + "@axon.test"

	first, err := c.signInWith(t, fakeIdentity(subject, address))
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}

	second, err := c.signInWith(t, fakeIdentity(subject, "renamed-"+address))
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}

	if second.GetCreated() {
		t.Error("a returning user was reported as newly created")
	}
	if second.GetUser().GetId() != first.GetUser().GetId() {
		t.Errorf("the same provider identity produced two accounts: %s and %s",
			first.GetUser().GetId(), second.GetUser().GetId())
	}
}

// Someone who registered with a password and later uses a provider is the same
// person. Safe only because the provider verified the address — see the next
// test for what happens when it has not.
func TestOAuthLinksToAnExistingPasswordAccount(t *testing.T) {
	c := newCaller(t)

	acct := c.register(t)

	result, err := c.signInWith(t, fakeIdentity("e2e-"+uuid.NewString(), acct.email))
	if err != nil {
		t.Fatalf("sign in over an existing account: %v", err)
	}

	if result.GetCreated() {
		t.Error("linking to an existing account was reported as a creation")
	}
	if result.GetUser().GetId() != acct.userID {
		t.Errorf("a second account was created for %s", acct.email)
	}

	// The password still works: linking a provider adds a way in, it does not
	// take one away.
	if _, err := c.login(acct.email, testPassword); err != nil {
		t.Errorf("the password stopped working after linking a provider: %v", err)
	}
}

// The account takeover the verified-address rule exists to stop.
func TestAnUnverifiedAddressCannotClaimAnAccount(t *testing.T) {
	c := newCaller(t)

	acct := c.register(t)

	_, err := c.signInWith(t, url.Values{
		"sub":            {"e2e-attacker-" + uuid.NewString()},
		"email":          {acct.email},
		"email_verified": {"false"},
	})
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("code = %v, want failed_precondition (error: %v)", connect.CodeOf(err), err)
	}

	// And the account it was aimed at is untouched.
	if _, err := c.login(acct.email, testPassword); err != nil {
		t.Errorf("the targeted account was disturbed: %v", err)
	}
}

// A state is spent once. The second callback carrying it is a replay.
func TestOAuthStateCannotBeReplayed(t *testing.T) {
	c := newCaller(t)

	started, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code, _ := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()),
		fakeIdentity("e2e-"+uuid.NewString(), "replay-"+uuid.NewString()+"@axon.test"))

	if _, err := c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), ""); err != nil {
		t.Fatalf("the first completion failed: %v", err)
	}

	_, err = c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), "")
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want invalid_argument", connect.CodeOf(err))
	}
}

// A state minted for one provider must not close a flow at another.
func TestOAuthStateIsBoundToItsProvider(t *testing.T) {
	c := newCaller(t)

	started, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code, _ := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()),
		fakeIdentity("e2e-"+uuid.NewString(), "crossed-"+uuid.NewString()+"@axon.test"))

	// Google is not configured in this stack, so it is refused as unsupported
	// before the state is even looked at. Either refusal is correct; completing
	// the flow is not.
	if _, err = c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_GOOGLE, code, started.GetState(), ""); err == nil {
		t.Fatal("a state issued for one provider completed a flow at another")
	}

	// And the state survived that attempt, so the legitimate callback still
	// works: a failed cross-provider attempt must not consume someone's sign-in.
	if _, err := c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), ""); err != nil {
		t.Fatalf("the genuine callback was spent by the refused one: %v", err)
	}
}

// Nobody configured Google or GitHub in this stack, so they must be absent
// rather than present and broken.
func TestUnconfiguredProvidersAreRefused(t *testing.T) {
	c := newCaller(t)

	for _, provider := range []authv1.OauthProvider{
		authv1.OauthProvider_OAUTH_PROVIDER_GOOGLE,
		authv1.OauthProvider_OAUTH_PROVIDER_GITHUB,
		// Apple is not implemented yet and has to fail the same way.
		authv1.OauthProvider_OAUTH_PROVIDER_APPLE,
	} {
		_, err := c.startOAuth(provider, "")
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("StartOAuth(%v) = %v, want invalid_argument", provider, connect.CodeOf(err))
		}
	}
}

// An open redirect here is how an attacker collects authorization codes, so the
// destination is checked before the browser ever leaves.
func TestReturnToOutsideTheAllowListIsRefused(t *testing.T) {
	c := newCaller(t)

	_, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "https://evil.example/collect")
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want invalid_argument", connect.CodeOf(err))
	}
}

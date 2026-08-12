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

func t0() context.Context { return context.Background() }

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

func (c *caller) signInWith(t *testing.T, extra url.Values) (*authv1.CompleteOAuthResponse, error) {
	t.Helper()

	return c.signInAs(t, extra, "")
}

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

	if returned != started.GetState() {
		t.Fatalf("state came back as %q, want %q", returned, started.GetState())
	}

	return c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), displayName)
}

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

	user, err := c.getMe(result.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with an OAuth token: %v", err)
	}
	if user.GetId() != result.GetUser().GetId() {
		t.Errorf("GetMe returned a different user than the sign-in did")
	}
}

func TestTheClientsNameReachesTheAccountItCreates(t *testing.T) {
	c := newCaller(t)

	subject := "e2e-" + uuid.NewString()
	address := subject + "@axon.test"
	const name = "Ada Lovelace"

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

	if _, err := c.login(acct.email, testPassword); err != nil {
		t.Errorf("the password stopped working after linking a provider: %v", err)
	}
}

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

	if _, err := c.login(acct.email, testPassword); err != nil {
		t.Errorf("the targeted account was disturbed: %v", err)
	}
}

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

func TestOAuthStateIsBoundToItsProvider(t *testing.T) {
	c := newCaller(t)

	started, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	code, _ := consent(t, rewriteForRunner(t, started.GetAuthorizationUrl()),
		fakeIdentity("e2e-"+uuid.NewString(), "crossed-"+uuid.NewString()+"@axon.test"))

	if _, err = c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_GOOGLE, code, started.GetState(), ""); err == nil {
		t.Fatal("a state issued for one provider completed a flow at another")
	}

	if _, err := c.completeOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, code, started.GetState(), ""); err != nil {
		t.Fatalf("the genuine callback was spent by the refused one: %v", err)
	}
}

func TestUnconfiguredProvidersAreRefused(t *testing.T) {
	c := newCaller(t)

	for _, provider := range []authv1.OauthProvider{
		authv1.OauthProvider_OAUTH_PROVIDER_GOOGLE,
		authv1.OauthProvider_OAUTH_PROVIDER_GITHUB,

		authv1.OauthProvider_OAUTH_PROVIDER_APPLE,
	} {
		_, err := c.startOAuth(provider, "")
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("StartOAuth(%v) = %v, want invalid_argument", provider, connect.CodeOf(err))
		}
	}
}

func TestReturnToOutsideTheAllowListIsRefused(t *testing.T) {
	c := newCaller(t)

	_, err := c.startOAuth(authv1.OauthProvider_OAUTH_PROVIDER_FAKE, "https://evil.example/collect")
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want invalid_argument", connect.CodeOf(err))
	}
}

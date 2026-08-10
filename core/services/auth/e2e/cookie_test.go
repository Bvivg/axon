//go:build e2e

package e2e

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

// refreshCookieName is the cookie the gateway keeps a browser's refresh token
// in. It is spelled out rather than imported: the gateway's package is internal
// to another module, and a browser does not import Go packages either. If this
// name has to change, a client somewhere has to change with it, and a test that
// only compiles against a constant would not say so.
const refreshCookieName = "axon_refresh"

// browserOrigin is the origin the e2e gateway's CORS allow-list carries.
const browserOrigin = "http://localhost:3000"

// browser is a caller that behaves the way a browser does: it announces its
// origin and it carries the cookies it has been given.
//
// Cookies are tracked by hand rather than with net/http/cookiejar because the
// gateway runs with Secure on — the e2e stack sets APP_ENV=test, which is the
// production-shaped default — and a jar would correctly refuse to send a Secure
// cookie back over the plaintext compose network. Doing it manually keeps the
// shipping configuration under test and lets the attribute itself be asserted.
type browser struct {
	*caller

	// held is the refresh token value currently stored, or "" for none.
	held string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	return &browser{caller: newCaller(t)}
}

// asBrowser marks a request as coming from a browser and attaches whatever
// cookie is currently held.
func asBrowser[T any](b *browser, req *connect.Request[T]) *connect.Request[T] {
	req.Header().Set("Origin", browserOrigin)
	if b.held != "" {
		req.Header().Set("Cookie", (&http.Cookie{Name: refreshCookieName, Value: b.held}).String())
	}
	return req
}

// take applies the Set-Cookie a response carried, the way a browser would, and
// returns the cookie for inspection. It returns nil if the response set none.
func (b *browser) take(header http.Header) *http.Cookie {
	for _, c := range (&http.Response{Header: header}).Cookies() {
		if c.Name != refreshCookieName {
			continue
		}
		if c.MaxAge < 0 || c.Value == "" {
			b.held = ""
		} else {
			b.held = c.Value
		}
		return c
	}
	return nil
}

// registerAsBrowser signs up the way the web client does.
func (b *browser) register(t *testing.T) (*authv1.RegisterResponse, *http.Cookie) {
	t.Helper()

	displayName := "E2E Browser"
	resp, err := b.client.Register(context.Background(), asBrowser(b, connect.NewRequest(&authv1.RegisterRequest{
		Email:       email(),
		Password:    testPassword,
		DisplayName: &displayName,
	})))
	if err != nil {
		t.Fatalf("register as a browser: %v", err)
	}

	return resp.Msg, b.take(resp.Header())
}

// The reason the whole mechanism exists: script on the page must never be able
// to read the long-lived half of the token pair.
func TestBrowserNeverReceivesTheRefreshTokenInTheBody(t *testing.T) {
	b := newBrowser(t)

	msg, c := b.register(t)

	if got := msg.GetTokens().GetRefreshToken(); got != "" {
		t.Errorf("the response body carries a refresh token (%q); any XSS on the page now has it", got)
	}
	// The short-lived half still comes back in the body — the client has to put
	// it in the Authorization header, so it has to be able to see it.
	if msg.GetTokens().GetAccessToken() == "" {
		t.Error("no access token in the body; the client has nothing to call with")
	}

	if c == nil {
		t.Fatal("no refresh cookie was set, so the browser cannot refresh at all")
	}
	if !c.HttpOnly {
		t.Error("the cookie is not HttpOnly, which defeats moving the token at all")
	}
	if !c.Secure {
		t.Error("the cookie is not Secure; APP_ENV=test should give the production-shaped default")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/axon.auth.v1.AuthService/" {
		t.Errorf("Path = %q, want the auth procedures only", c.Path)
	}
}

// The browser refreshes with a request body it cannot fill in, because it has
// never seen the token. The cookie has to be enough on its own.
func TestBrowserRefreshesWithTheCookieAlone(t *testing.T) {
	b := newBrowser(t)

	first, _ := b.register(t)

	resp, err := b.client.RefreshToken(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.RefreshTokenRequest{})))
	if err != nil {
		t.Fatalf("refresh with nothing but the cookie: %v", err)
	}

	if got := resp.Msg.GetTokens().GetRefreshToken(); got != "" {
		t.Errorf("the rotated token came back in the body (%q)", got)
	}

	rotated := b.take(resp.Header())
	if rotated == nil || rotated.Value == "" {
		t.Fatal("the refresh did not hand back a new cookie, so the next one has nothing to send")
	}

	// The access token it issued is a real one.
	user, err := b.getMe(resp.Msg.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with the refreshed access token: %v", err)
	}
	if user.GetId() != first.GetUser().GetId() {
		t.Error("the refresh returned a token for a different user")
	}
}

// Signing out has to take the cookie away as well as revoke the chain.
// Otherwise the browser keeps presenting a token that can only be refused, and
// the next visit looks like a broken session rather than a signed-out one.
func TestBrowserLogoutClearsTheCookieAndTheChain(t *testing.T) {
	b := newBrowser(t)

	b.register(t)

	// Kept so the chain can be probed from outside afterwards. A browser never
	// sees this value; the test does, because it is standing in for one.
	revoked := b.held
	if revoked == "" {
		t.Fatal("registration left the browser without a cookie")
	}

	resp, err := b.client.Logout(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.LogoutRequest{})))
	if err != nil {
		t.Fatalf("logout with nothing but the cookie: %v", err)
	}

	c := b.take(resp.Header())
	if c == nil {
		t.Fatal("logout set no cookie, so the browser keeps the revoked one")
	}
	if c.MaxAge >= 0 || c.Value != "" {
		t.Errorf("cookie = %q with MaxAge %d, want an expiry", c.Value, c.MaxAge)
	}
	if b.held != "" {
		t.Error("a browser applying that Set-Cookie would still be holding a token")
	}

	// And the chain really is dead, not merely forgotten by the client.
	if _, err := newCaller(t).refresh(revoked); err == nil {
		t.Error("the revoked token still refreshes; logout only cleared the cookie")
	}
}

// Provider sign-in mints the same pair, so it has to be handled the same way.
func TestBrowserOAuthSignInUsesTheCookie(t *testing.T) {
	b := newBrowser(t)

	started, err := b.client.StartOAuth(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.StartOAuthRequest{
			Provider: authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
		})))
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}

	subject := "e2e-browser-" + uuid.NewString()
	code, _ := consent(t, rewriteForRunner(t, started.Msg.GetAuthorizationUrl()),
		url.Values{"sub": {subject}, "email": {subject + "@axon.test"}})

	completed, err := b.client.CompleteOAuth(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.CompleteOAuthRequest{
			Provider: authv1.OauthProvider_OAUTH_PROVIDER_FAKE,
			Code:     code,
			State:    started.Msg.GetState(),
		})))
	if err != nil {
		t.Fatalf("CompleteOAuth: %v", err)
	}

	if got := completed.Msg.GetTokens().GetRefreshToken(); got != "" {
		t.Errorf("provider sign-in left the refresh token in the body (%q)", got)
	}
	if c := b.take(completed.Header()); c == nil || c.Value == "" {
		t.Fatal("provider sign-in set no refresh cookie")
	}

	// The access token works, and the cookie refreshes — the same session a
	// password sign-in produces, by the same route.
	if _, err := b.getMe(completed.Msg.GetTokens().GetAccessToken()); err != nil {
		t.Fatalf("GetMe after provider sign-in: %v", err)
	}
	if _, err := b.client.RefreshToken(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.RefreshTokenRequest{}))); err != nil {
		t.Fatalf("refresh after provider sign-in: %v", err)
	}
}

// The other half of the bargain. A client that is not a browser — the SwiftUI
// app, and every test above this file — must be untouched by any of it.
func TestNonBrowserClientsStillReceiveTheTokenInTheBody(t *testing.T) {
	c := newCaller(t)

	acct := c.register(t)

	if acct.tokens.GetRefreshToken() == "" {
		t.Fatal("a non-browser client got no refresh token; the cookie path leaked into it")
	}
	if _, err := c.refresh(acct.tokens.GetRefreshToken()); err != nil {
		t.Errorf("the token in the body does not work: %v", err)
	}
}

// A browser cannot opt out of the cookie by leaving Origin off — it is not
// allowed to — but it can try to smuggle a token in the body. That must not be
// a second way to present the credential.
func TestABrowsersBodyTokenIsIgnored(t *testing.T) {
	victim := newCaller(t)
	acct := victim.register(t)

	attacker := newBrowser(t)

	_, err := attacker.client.RefreshToken(context.Background(),
		asBrowser(attacker, connect.NewRequest(&authv1.RefreshTokenRequest{
			RefreshToken: acct.tokens.GetRefreshToken(),
		})))
	if err == nil {
		t.Fatal("a refresh token in a browser's request body was accepted")
	}

	// And the victim's token is untouched: the attempt must not have consumed it.
	if _, err := victim.refresh(acct.tokens.GetRefreshToken()); err != nil {
		t.Errorf("the refused attempt still spent the victim's token: %v", err)
	}
}

// The access token is unaffected by all of this: it stays a header, because a
// cookie sent automatically on every call is what CSRF is made of.
func TestTheAccessTokenIsStillABearerHeader(t *testing.T) {
	b := newBrowser(t)

	msg, _ := b.register(t)

	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set("Origin", browserOrigin)
	req.Header().Set(authn.Header, "Bearer "+msg.GetTokens().GetAccessToken())

	if _, err := b.client.GetMe(context.Background(), req); err != nil {
		t.Fatalf("GetMe with a bearer header from a browser: %v", err)
	}
}

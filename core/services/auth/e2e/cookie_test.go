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

const refreshCookieName = "axon_refresh"

const browserOrigin = "http://localhost:3000"

type browser struct {
	*caller

	held string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	return &browser{caller: newCaller(t)}
}

func asBrowser[T any](b *browser, req *connect.Request[T]) *connect.Request[T] {
	req.Header().Set("Origin", browserOrigin)
	if b.held != "" {
		req.Header().Set("Cookie", (&http.Cookie{Name: refreshCookieName, Value: b.held}).String())
	}
	return req
}

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

func TestBrowserNeverReceivesTheRefreshTokenInTheBody(t *testing.T) {
	b := newBrowser(t)

	msg, c := b.register(t)

	if got := msg.GetTokens().GetRefreshToken(); got != "" {
		t.Errorf("the response body carries a refresh token (%q); any XSS on the page now has it", got)
	}

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

	user, err := b.getMe(resp.Msg.GetTokens().GetAccessToken())
	if err != nil {
		t.Fatalf("GetMe with the refreshed access token: %v", err)
	}
	if user.GetId() != first.GetUser().GetId() {
		t.Error("the refresh returned a token for a different user")
	}
}

func TestBrowserLogoutClearsTheCookieAndTheChain(t *testing.T) {
	b := newBrowser(t)

	b.register(t)

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

	if _, err := newCaller(t).refresh(revoked); err == nil {
		t.Error("the revoked token still refreshes; logout only cleared the cookie")
	}
}

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

	if _, err := b.getMe(completed.Msg.GetTokens().GetAccessToken()); err != nil {
		t.Fatalf("GetMe after provider sign-in: %v", err)
	}
	if _, err := b.client.RefreshToken(context.Background(),
		asBrowser(b, connect.NewRequest(&authv1.RefreshTokenRequest{}))); err != nil {
		t.Fatalf("refresh after provider sign-in: %v", err)
	}
}

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

	if _, err := victim.refresh(acct.tokens.GetRefreshToken()); err != nil {
		t.Errorf("the refused attempt still spent the victim's token: %v", err)
	}
}

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

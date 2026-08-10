package oauth

// This file is in the package rather than beside it: the provider endpoints are
// package variables so a test can redirect them at a stand-in, and exporting
// them purely for tests would put a mutable global on the package's API.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

// stubProvider stands in for a real identity provider: it answers the token
// endpoint and whatever profile endpoints the provider under test calls.
//
// This is not a mock of the Provider interface. The code under test performs a
// genuine OAuth2 exchange over HTTP and parses a genuine response body, which is
// where the bugs in a provider integration actually live.
type stubProvider struct {
	server *httptest.Server
	routes map[string]any
}

func newStub(t *testing.T, routes map[string]any) *stubProvider {
	t.Helper()

	stub := &stubProvider{routes: routes}

	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		// The exchange must carry the PKCE verifier. A provider would refuse
		// without it, and so does this.
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("code_verifier") == "" {
			http.Error(w, "code_verifier is missing", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "stub-access-token",
			"token_type":   "Bearer",
		})
	})

	for path, body := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer stub-access-token" {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(body)
		})
	}

	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)

	return stub
}

func (s *stubProvider) url(path string) string { return s.server.URL + path }

// pointProviderAt rewires a provider's endpoints at the stub for one test.
func pointProviderAt(t *testing.T, p Provider, stub *stubProvider) {
	t.Helper()

	hp, ok := p.(*httpProvider)
	if !ok {
		t.Fatalf("provider %T is not an httpProvider", p)
	}
	hp.cfg.Endpoint.AuthURL = stub.url("/authorize")
	hp.cfg.Endpoint.TokenURL = stub.url("/token")
}

func TestGoogleProfile(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/userinfo": map[string]any{
			"sub":            "google-subject-1",
			"email":          "person@example.com",
			"email_verified": true,
			"name":           "A Person",
			"picture":        "https://example.com/avatar.png",
		},
	})

	original := googleUserInfoURL
	googleUserInfoURL = stub.url("/userinfo")
	t.Cleanup(func() { googleUserInfoURL = original })

	provider := newGoogle("client", "secret", "http://localhost:3000/auth/callback/google")
	pointProviderAt(t, provider, stub)

	profile, err := provider.Exchange(context.Background(), "a-code", "a-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if profile.ProviderUserID != "google-subject-1" {
		t.Errorf("subject = %q, want the sub claim", profile.ProviderUserID)
	}
	if profile.Email != "person@example.com" {
		t.Errorf("email = %q", profile.Email)
	}
	if !profile.EmailVerified {
		t.Error("email_verified was not carried through")
	}
	if profile.DisplayName != "A Person" {
		t.Errorf("display name = %q", profile.DisplayName)
	}
	if profile.AvatarURL != "https://example.com/avatar.png" {
		t.Errorf("avatar = %q", profile.AvatarURL)
	}
}

// GitHub returns email: null for anyone who keeps their address private, which
// is a setting rather than an edge case. The primary verified address from
// /user/emails is the real answer.
func TestGitHubTakesThePrimaryVerifiedAddress(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/user": map[string]any{
			"id":         12345,
			"login":      "octocat",
			"name":       "The Octocat",
			"avatar_url": "https://github.example/avatar.png",
			"email":      nil,
		},
		"/user/emails": []map[string]any{
			{"email": "secondary@example.com", "primary": false, "verified": true},
			{"email": "primary@example.com", "primary": true, "verified": true},
			{"email": "old@example.com", "primary": false, "verified": false},
		},
	})

	profile := exchangeWithGitHub(t, stub)

	if profile.Email != "primary@example.com" {
		t.Errorf("email = %q, want the primary address", profile.Email)
	}
	if !profile.EmailVerified {
		t.Error("the primary address is verified but did not come through as such")
	}
	// The numeric id, never the login: logins can be changed and reused.
	if profile.ProviderUserID != "12345" {
		t.Errorf("subject = %q, want the numeric id", profile.ProviderUserID)
	}
	if profile.DisplayName != "The Octocat" {
		t.Errorf("display name = %q", profile.DisplayName)
	}
}

// A verified secondary address must not stand in for an unverified primary.
// Otherwise someone whose primary address is unconfirmed signs in as an identity
// they did not choose.
func TestGitHubRefusesAnUnverifiedPrimaryAddress(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/user": map[string]any{"id": 99, "login": "someone"},
		"/user/emails": []map[string]any{
			{"email": "verified-secondary@example.com", "primary": false, "verified": true},
			{"email": "unverified-primary@example.com", "primary": true, "verified": false},
		},
	})

	_, err := exchangeWithGitHubErr(t, stub)
	if !errors.Is(err, domain.ErrOauthEmailUnverified) {
		t.Fatalf("err = %v, want ErrOauthEmailUnverified", err)
	}
}

// GitHub's display name is optional. Their own UI falls back to the login.
func TestGitHubFallsBackToTheLogin(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/user": map[string]any{"id": 7, "login": "octocat", "name": ""},
		"/user/emails": []map[string]any{
			{"email": "octocat@example.com", "primary": true, "verified": true},
		},
	})

	if profile := exchangeWithGitHub(t, stub); profile.DisplayName != "octocat" {
		t.Errorf("display name = %q, want the login", profile.DisplayName)
	}
}

// An account with no addresses visible to us cannot be matched to a user.
func TestGitHubRefusesAProfileWithNoAddress(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/user":        map[string]any{"id": 7, "login": "octocat"},
		"/user/emails": []map[string]any{},
	})

	_, err := exchangeWithGitHubErr(t, stub)
	if !errors.Is(err, domain.ErrOauthProfileIncomplete) {
		t.Fatalf("err = %v, want ErrOauthProfileIncomplete", err)
	}
}

// The check that makes matching on email safe at all. Without it, registering
// victim@example.com at a provider that never confirms addresses is enough to
// inherit their account here.
func TestAnUnverifiedAddressIsRefused(t *testing.T) {
	stub := newStub(t, map[string]any{
		"/userinfo": map[string]any{
			"sub":            "google-subject-2",
			"email":          "unconfirmed@example.com",
			"email_verified": false,
		},
	})

	original := googleUserInfoURL
	googleUserInfoURL = stub.url("/userinfo")
	t.Cleanup(func() { googleUserInfoURL = original })

	provider := newGoogle("client", "secret", "http://localhost:3000/auth/callback/google")
	pointProviderAt(t, provider, stub)

	_, err := provider.Exchange(context.Background(), "a-code", "a-verifier")
	if !errors.Is(err, domain.ErrOauthEmailUnverified) {
		t.Fatalf("err = %v, want ErrOauthEmailUnverified", err)
	}
}

func TestAuthorizationURLCarriesPKCE(t *testing.T) {
	provider := newGoogle("client-id", "secret", "http://localhost:3000/auth/callback/google")

	url := provider.AuthorizationURL("the-state", "the-challenge")

	for _, want := range []string{
		"state=the-state",
		"code_challenge=the-challenge",
		"code_challenge_method=S256",
		"client_id=client-id",
	} {
		if !strings.Contains(url, want) {
			t.Errorf("authorization URL is missing %q: %s", want, url)
		}
	}
}

// A provider having a bad day must fail the sign-in rather than be mistaken for
// a bad token.
func TestAProviderErrorIsNotMistakenForABadProfile(t *testing.T) {
	stub := newStub(t, nil)

	original := googleUserInfoURL
	googleUserInfoURL = stub.url("/userinfo") // never registered: answers 404
	t.Cleanup(func() { googleUserInfoURL = original })

	provider := newGoogle("client", "secret", "http://localhost:3000/auth/callback/google")
	pointProviderAt(t, provider, stub)

	_, err := provider.Exchange(context.Background(), "a-code", "a-verifier")
	switch {
	case err == nil:
		t.Fatal("a failing provider produced a profile")
	case errors.Is(err, domain.ErrOauthEmailUnverified), errors.Is(err, domain.ErrOauthProfileIncomplete):
		t.Fatalf("an outage was reported as a profile problem: %v", err)
	}
}

func exchangeWithGitHub(t *testing.T, stub *stubProvider) domain.ProviderProfile {
	t.Helper()

	profile, err := exchangeWithGitHubErr(t, stub)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	return profile
}

func exchangeWithGitHubErr(t *testing.T, stub *stubProvider) (domain.ProviderProfile, error) {
	t.Helper()

	originalUser, originalEmails := githubUserURL, githubEmailsURL
	githubUserURL, githubEmailsURL = stub.url("/user"), stub.url("/user/emails")
	t.Cleanup(func() { githubUserURL, githubEmailsURL = originalUser, originalEmails })

	provider := newGitHub("client", "secret", "http://localhost:3000/auth/callback/github")
	pointProviderAt(t, provider, stub)

	return provider.Exchange(context.Background(), "a-code", "a-verifier")
}

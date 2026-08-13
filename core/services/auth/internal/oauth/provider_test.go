package oauth

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

type stubProvider struct {
	server *httptest.Server
	routes map[string]any
}

func newStub(t *testing.T, routes map[string]any) *stubProvider {
	t.Helper()

	stub := &stubProvider{routes: routes}

	mux := http.NewServeMux()

	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {

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

	if profile.ProviderUserID != "12345" {
		t.Errorf("subject = %q, want the numeric id", profile.ProviderUserID)
	}
	if profile.DisplayName != "The Octocat" {
		t.Errorf("display name = %q", profile.DisplayName)
	}
}

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

func TestAProviderErrorIsNotMistakenForABadProfile(t *testing.T) {
	stub := newStub(t, nil)

	original := googleUserInfoURL
	googleUserInfoURL = stub.url("/userinfo")
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

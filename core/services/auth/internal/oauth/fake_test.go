package oauth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

func newFakeProvider(t *testing.T) oauth.Provider {
	t.Helper()

	server := httptest.NewServer(oauth.FakeAuthorizeHandler())
	t.Cleanup(server.Close)

	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  "http://localhost:3000/auth/callback",
		FakeEnabled:      true,
		FakeAuthorizeURL: server.URL,
		Environment:      config.EnvTest,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	provider, err := registry.Get(domain.ProviderFake)
	if err != nil {
		t.Fatalf("Get(fake): %v", err)
	}
	return provider
}

func authorize(t *testing.T, rawURL string) url.Values {
	t.Helper()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	return location.Query()
}

func TestFakeProviderCompletesTheFlow(t *testing.T) {
	provider := newFakeProvider(t)

	authURL := provider.AuthorizationURL("the-state", oauth.Challenge("a-verifier"))
	callback := authorize(t, authURL)

	if got := callback.Get("state"); got != "the-state" {
		t.Errorf("state = %q, want it returned unchanged", got)
	}

	code := callback.Get("code")
	if code == "" {
		t.Fatal("no code was returned")
	}

	profile, err := provider.Exchange(context.Background(), code, "a-verifier")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if profile.ProviderUserID == "" || profile.Email == "" {
		t.Errorf("the profile is incomplete: %+v", profile)
	}
	if !profile.EmailVerified {
		t.Error("the default identity should be verified")
	}
}

func TestFakeProviderHonoursTheRequestedIdentity(t *testing.T) {
	provider := newFakeProvider(t)

	authURL := provider.AuthorizationURL("s", oauth.Challenge("v")) +
		"&sub=subject-42&email=chosen@fake.axon.test&name=Chosen+Person"

	callback := authorize(t, authURL)

	profile, err := provider.Exchange(context.Background(), callback.Get("code"), "v")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if profile.ProviderUserID != "subject-42" {
		t.Errorf("subject = %q", profile.ProviderUserID)
	}
	if profile.Email != "chosen@fake.axon.test" {
		t.Errorf("email = %q", profile.Email)
	}
	if profile.DisplayName != "Chosen Person" {
		t.Errorf("display name = %q", profile.DisplayName)
	}
}

func TestFakeProviderCanReturnAnUnverifiedAddress(t *testing.T) {
	provider := newFakeProvider(t)

	authURL := provider.AuthorizationURL("s", oauth.Challenge("v")) + "&email_verified=false"
	callback := authorize(t, authURL)

	_, err := provider.Exchange(context.Background(), callback.Get("code"), "v")
	if !errors.Is(err, domain.ErrOauthEmailUnverified) {
		t.Fatalf("err = %v, want ErrOauthEmailUnverified", err)
	}
}

func TestFakeProviderRejectsAGarbageCode(t *testing.T) {
	provider := newFakeProvider(t)

	for _, code := range []string{"", "not-base64!!", "aGVsbG8"} {
		if _, err := provider.Exchange(context.Background(), code, "v"); err == nil {
			t.Errorf("code %q was accepted", code)
		}
	}
}

func TestFakeAuthorizeNeedsSomewhereToGo(t *testing.T) {
	server := httptest.NewServer(oauth.FakeAuthorizeHandler())
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"?state=s", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 without a redirect_uri", resp.StatusCode)
	}
}

func fakeCode(t *testing.T, profile map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("encode profile: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func TestFakeProviderOnlyVouchesForItsReservedDomain(t *testing.T) {
	provider := newFakeProvider(t)

	for email, allowed := range map[string]bool{
		"someone@fake.axon.test":            true,
		"Someone@Fake.Axon.Test":            true,
		"victim@example.com":                false,
		"victim@axon.test":                  false,
		"victim@notfake.axon.test":          false,
		"victim@sub.fake.axon.test":         false,
		"victim@fake.axon.test.evil":        false,
		"victim@example.com@fake.axon.test": false,
		"@fake.axon.test":                   false,
	} {
		t.Run(email, func(t *testing.T) {
			code := fakeCode(t, map[string]any{"ProviderUserID": "atk", "Email": email, "EmailVerified": true})

			_, err := provider.Exchange(context.Background(), code, "v")
			if allowed && err != nil {
				t.Fatalf("Exchange(%s) = %v, want it accepted", email, err)
			}
			if !allowed && !errors.Is(err, domain.ErrOauthEmailNotAllowed) {
				t.Fatalf("Exchange(%s) = %v, want ErrOauthEmailNotAllowed", email, err)
			}
		})
	}
}

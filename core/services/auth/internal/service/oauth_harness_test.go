package service_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

const webOrigin = "http://localhost:3000"

func withOAuth(t *testing.T) harnessOption {
	t.Helper()

	authorize := httptest.NewServer(oauth.FakeAuthorizeHandler())
	t.Cleanup(authorize.Close)

	redis := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: redis.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  webOrigin + "/auth/callback",
		FakeEnabled:      true,
		FakeAuthorizeURL: authorize.URL,

		Google: oauth.ProviderConfig{ClientID: "an-id", ClientSecret: "a-secret"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	states, err := oauth.NewStateStore(client, oauth.StateStoreConfig{})
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	returnTo, err := oauth.NewReturnToPolicy([]string{webOrigin})
	if err != nil {
		t.Fatalf("NewReturnToPolicy: %v", err)
	}

	return func(cfg *service.Config) {
		cfg.OAuth = &service.OAuthConfig{
			Providers: registry,
			States:    states,
			ReturnTo:  returnTo,
		}
	}
}

func (h *harness) signIn(t *testing.T, identity url.Values) service.CompleteOAuthResult {
	t.Helper()

	result, err := h.trySignIn(t, identity)
	if err != nil {
		t.Fatalf("sign in with the fake provider: %v", err)
	}
	return result
}

func (h *harness) trySignIn(t *testing.T, identity url.Values) (service.CompleteOAuthResult, error) {
	t.Helper()

	return h.trySignInAs(t, identity, "")
}

func (h *harness) trySignInAs(
	t *testing.T,
	identity url.Values,
	displayName string,
) (service.CompleteOAuthResult, error) {
	t.Helper()

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		return service.CompleteOAuthResult{}, err
	}

	code := h.follow(t, started.AuthorizationURL, identity)

	return h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider:    domain.ProviderFake,
		Code:        code,
		State:       started.State,
		DisplayName: displayName,
	})
}

func (h *harness) follow(t *testing.T, authorizationURL string, identity url.Values) string {
	t.Helper()

	target := authorizationURL
	if len(identity) > 0 {
		target += "&" + identity.Encode()
	}

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("build authorization request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the provider answered %d, want a redirect", resp.StatusCode)
	}

	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse the callback URL: %v", err)
	}

	code := location.Query().Get("code")
	if code == "" {
		t.Fatal("the callback carried no code")
	}
	return code
}

func identity(subject, email string) url.Values {
	return url.Values{"sub": {subject}, "email": {email}}
}

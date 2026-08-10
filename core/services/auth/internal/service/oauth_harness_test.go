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

// withOAuth wires provider sign-in into the service under test, using the fake
// provider over a real HTTP redirect. Nothing here is a mock of this service's
// own code: the flow that runs is the flow that ships.
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
		// Google is configured too, so a test can prove that a state minted for
		// one provider cannot close a flow at another. Nothing calls out to it.
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

// signIn walks a whole provider sign-in: start, follow the redirect the way a
// browser would, and complete. identity carries the query the fake provider
// reads, so a test can choose who is signing in.
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

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		return service.CompleteOAuthResult{}, err
	}

	code := h.follow(t, started.AuthorizationURL, identity)

	return h.svc.CompleteOAuth(t.Context(), domain.ProviderFake, code, started.State)
}

// follow performs the browser's half: request the authorization URL, stop at the
// redirect, and read the code out of it.
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

// identity is shorthand for the fake provider's query parameters.
func identity(subject, email string) url.Values {
	return url.Values{"sub": {subject}, "email": {email}}
}

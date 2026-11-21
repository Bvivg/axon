package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/shared/pkg/config"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

const webOrigin = "http://localhost:3000"

type scriptedProvider struct {
	id domain.Provider
}

func (p scriptedProvider) ID() domain.Provider { return p.id }

func (p scriptedProvider) AuthorizationURL(state, _ string) string {
	return "https://" + p.id.String() + ".provider.test/authorize?state=" + url.QueryEscape(state)
}

func (p scriptedProvider) Exchange(_ context.Context, code, _ string) (domain.ProviderProfile, error) {
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return domain.ProviderProfile{}, domain.ErrOauthProfileIncomplete
	}
	var profile domain.ProviderProfile
	if err := json.Unmarshal(raw, &profile); err != nil || profile.ProviderUserID == "" || profile.Email == "" {
		return domain.ProviderProfile{}, domain.ErrOauthProfileIncomplete
	}
	if !profile.EmailVerified {
		return domain.ProviderProfile{}, domain.ErrOauthEmailUnverified
	}
	return profile, nil
}

type providerSet struct {
	registry *oauth.Registry
	scripted map[domain.Provider]scriptedProvider
}

func (s providerSet) Get(id domain.Provider) (oauth.Provider, error) {
	if p, ok := s.scripted[id]; ok {
		return p, nil
	}
	return s.registry.Get(id)
}

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
		Environment:      config.EnvTest,
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

	providers := providerSet{
		registry: registry,
		scripted: map[domain.Provider]scriptedProvider{
			domain.ProviderGoogle: {id: domain.ProviderGoogle},
			domain.ProviderGitHub: {id: domain.ProviderGitHub},
		},
	}

	return func(cfg *service.Config) {
		cfg.OAuth = &service.OAuthConfig{
			Providers: providers,
			States:    states,
			ReturnTo:  returnTo,
		}
	}
}

func (h *harness) signIn(t *testing.T, identity url.Values) service.CompleteOAuthResult {
	t.Helper()

	result, err := h.trySignIn(t, identity)
	if err != nil {
		t.Fatalf("sign in with the provider: %v", err)
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

	return h.trySignInWith(t, domain.ProviderGoogle, identity, displayName)
}

func (h *harness) trySignInWith(
	t *testing.T,
	provider domain.Provider,
	identity url.Values,
	displayName string,
) (service.CompleteOAuthResult, error) {
	t.Helper()

	started, err := h.svc.StartOAuth(t.Context(), provider, "")
	if err != nil {
		return service.CompleteOAuthResult{}, err
	}

	return h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider:    provider,
		Code:        scriptedCode(t, identity),
		State:       started.State,
		DisplayName: displayName,
	})
}

func (h *harness) trySignInFake(t *testing.T, identity url.Values) (service.CompleteOAuthResult, error) {
	t.Helper()

	started, err := h.svc.StartOAuth(t.Context(), domain.ProviderFake, "")
	if err != nil {
		return service.CompleteOAuthResult{}, err
	}

	code := h.follow(t, started.AuthorizationURL, identity)

	return h.svc.CompleteOAuth(t.Context(), service.CompleteOAuthInput{
		Provider: domain.ProviderFake,
		Code:     code,
		State:    started.State,
	})
}

func scriptedCode(t *testing.T, identity url.Values) string {
	t.Helper()

	raw, err := json.Marshal(domain.ProviderProfile{
		ProviderUserID: identity.Get("sub"),
		Email:          identity.Get("email"),
		EmailVerified:  identity.Get("email_verified") != "false",
		DisplayName:    identity.Get("name"),
		AvatarURL:      identity.Get("avatar_url"),
	})
	if err != nil {
		t.Fatalf("encode the provider profile: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
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

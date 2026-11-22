package oauth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

const redirectBase = "http://localhost:3000/auth/callback"

func configured() oauth.ProviderConfig {
	return oauth.ProviderConfig{ClientID: "an-id", ClientSecret: "a-secret"}
}

func TestConfiguredProvidersAreAvailable(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: redirectBase,
		Google:          configured(),
		GitHub:          configured(),
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, id := range []domain.Provider{domain.ProviderGoogle, domain.ProviderGitHub} {
		provider, err := registry.Get(id)
		if err != nil {
			t.Errorf("Get(%q): %v", id, err)
			continue
		}
		if provider.ID() != id {
			t.Errorf("Get(%q) returned provider %q", id, provider.ID())
		}
	}
}

func TestUnconfiguredProvidersAreUnsupported(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{RedirectBaseURL: redirectBase})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, id := range []domain.Provider{
		domain.ProviderGoogle,
		domain.ProviderGitHub,

		domain.ProviderApple,
		domain.ProviderFake,
	} {
		if _, err := registry.Get(id); !errors.Is(err, domain.ErrProviderUnsupported) {
			t.Errorf("Get(%q) = %v, want ErrProviderUnsupported", id, err)
		}
	}

	if got := registry.Available(); len(got) != 0 {
		t.Errorf("Available() = %v, want nothing", got)
	}
}

func TestHalfConfiguredProvidersAreTreatedAsAbsent(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: redirectBase,
		Google:          oauth.ProviderConfig{ClientID: "an-id"},
		GitHub:          oauth.ProviderConfig{ClientSecret: "a-secret"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if got := registry.Available(); len(got) != 0 {
		t.Errorf("Available() = %v, want nothing", got)
	}
}

func TestTheFakeProviderRunsOnlyInTestOrLoopbackDevelopment(t *testing.T) {
	const (
		loopbackRedirect  = "http://localhost:3003/auth/callback"
		loopbackAuthorize = "http://localhost:8081" + oauth.FakeAuthorizePath
	)

	for name, tc := range map[string]struct {
		env       config.Environment
		redirect  string
		authorize string
		allowed   bool
	}{
		"test on compose hostnames":        {env: config.EnvTest, redirect: "http://web.axon.test:3000/auth/callback", authorize: "http://auth.axon.test:8081" + oauth.FakeAuthorizePath, allowed: true},
		"development on localhost":         {env: config.EnvDevelopment, redirect: loopbackRedirect, authorize: loopbackAuthorize, allowed: true},
		"development on 127.0.0.1":         {env: config.EnvDevelopment, redirect: "http://127.0.0.1:3000/auth/callback", authorize: "http://127.0.0.1:28081" + oauth.FakeAuthorizePath, allowed: true},
		"development on ::1":               {env: config.EnvDevelopment, redirect: "http://[::1]:3000/auth/callback", authorize: "http://[::1]:8081" + oauth.FakeAuthorizePath, allowed: true},
		"production on localhost":          {env: config.EnvProduction, redirect: loopbackRedirect, authorize: loopbackAuthorize},
		"no environment":                   {redirect: loopbackRedirect, authorize: loopbackAuthorize},
		"development behind a LAN proxy":   {env: config.EnvDevelopment, redirect: "/auth/callback", authorize: oauth.FakeAuthorizePath},
		"development on a LAN address":     {env: config.EnvDevelopment, redirect: "https://192.168.2.1:3443/auth/callback", authorize: loopbackAuthorize},
		"development with a LAN authorize": {env: config.EnvDevelopment, redirect: loopbackRedirect, authorize: "https://axon.test" + oauth.FakeAuthorizePath},
		"development on a lookalike host":  {env: config.EnvDevelopment, redirect: "http://localhost.evil.example/auth/callback", authorize: loopbackAuthorize},
		"development with credentials":     {env: config.EnvDevelopment, redirect: "http://localhost@evil.example/auth/callback", authorize: loopbackAuthorize},
	} {
		t.Run(name, func(t *testing.T) {
			registry, err := oauth.NewRegistry(oauth.RegistryConfig{
				RedirectBaseURL:  tc.redirect,
				FakeEnabled:      true,
				FakeAuthorizeURL: tc.authorize,
				Environment:      tc.env,
			})
			if !tc.allowed {
				if err == nil {
					t.Fatal("the fake provider was accepted")
				}
				if !strings.Contains(err.Error(), "fake provider") {
					t.Errorf("the error does not say why: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			if _, err := registry.Get(domain.ProviderFake); err != nil {
				t.Fatalf("Get(fake): %v", err)
			}
		})
	}
}

func TestTheFakeProviderNeedsItsAuthorizeURL(t *testing.T) {
	_, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: redirectBase,
		FakeEnabled:     true,
		Environment:     config.EnvTest,
	})
	if err == nil {
		t.Fatal("the fake provider was accepted with nowhere to send the browser")
	}
}

func TestARedirectBaseURLIsRequired(t *testing.T) {
	if _, err := oauth.NewRegistry(oauth.RegistryConfig{Google: configured()}); err == nil {
		t.Fatal("a registry with no redirect base URL was accepted")
	}
}

func TestAvailableIsInAStableOrder(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  redirectBase,
		Google:           configured(),
		GitHub:           configured(),
		FakeEnabled:      true,
		FakeAuthorizeURL: "http://auth:8081" + oauth.FakeAuthorizePath,
		Environment:      config.EnvTest,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	want := []domain.Provider{domain.ProviderGoogle, domain.ProviderGitHub, domain.ProviderFake}

	for range 10 {
		got := registry.Available()
		if len(got) != len(want) {
			t.Fatalf("Available() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Available() = %v, want %v", got, want)
			}
		}
	}
}

func TestEachProviderRedirectsToItsOwnRoute(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{

		RedirectBaseURL: redirectBase + "/",
		Google:          configured(),
		GitHub:          configured(),
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for id, want := range map[domain.Provider]string{
		domain.ProviderGoogle: "auth%2Fcallback%2Fgoogle",
		domain.ProviderGitHub: "auth%2Fcallback%2Fgithub",
	} {
		provider, err := registry.Get(id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}

		url := provider.AuthorizationURL("a-state", "a-challenge")
		if !strings.Contains(url, want) {
			t.Errorf("%s redirect_uri is wrong: %s", id, url)
		}
	}
}

func TestTheAppVariantRedirectsUnderTheProviderRoute(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: redirectBase,
		Google:          configured(),
		GitHub:          configured(),
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for id, want := range map[domain.Provider]string{
		domain.ProviderGoogle: "auth%2Fcallback%2Fgoogle%2Fapp",
		domain.ProviderGitHub: "auth%2Fcallback%2Fgithub%2Fapp",
	} {
		provider, err := registry.App(id)
		if err != nil {
			t.Fatalf("App(%q): %v", id, err)
		}
		if url := provider.AuthorizationURL("a-state", "a-challenge"); !strings.Contains(url, want) {
			t.Errorf("%s app redirect_uri is wrong: %s", id, url)
		}
	}

	if _, err := registry.App(domain.ProviderApple); !errors.Is(err, domain.ErrProviderUnsupported) {
		t.Errorf("App(apple) = %v, want ErrProviderUnsupported", err)
	}
}

func TestTheCallbackFollowsTheSignInOrigin(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: "http://localhost:3003/auth/callback/",
		GitHub:          oauth.ProviderConfig{ClientID: "id", ClientSecret: "secret"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, tc := range []struct {
		id     domain.Provider
		origin string
		want   string
	}{
		{domain.ProviderGitHub, "https://axon.test", "https://axon.test/auth/callback/github"},
		{domain.ProviderGitHub, "https://axon.test:3443/", "https://axon.test:3443/auth/callback/github"},
		{domain.ProviderGitHub, "", ""},
		{domain.ProviderApple, "https://axon.test", ""},
	} {
		if got := registry.Callback(tc.id, tc.origin); got != tc.want {
			t.Errorf("Callback(%s, %q) = %q, want %q", tc.id, tc.origin, got, tc.want)
		}
	}
}

func TestARelativeRedirectBaseStillFollowsTheSignInOrigin(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: "/auth/callback",
		GitHub:          oauth.ProviderConfig{ClientID: "id", ClientSecret: "secret"},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got, want := registry.Callback(domain.ProviderGitHub, "https://axon.test"), "https://axon.test/auth/callback/github"; got != want {
		t.Errorf("Callback = %q, want %q", got, want)
	}
}

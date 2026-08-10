package oauth_test

import (
	"errors"
	"strings"
	"testing"

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

// A provider nobody set up must be absent, not present and broken. A registry
// that returned an authorization URL with an empty client id would send the
// person to a provider error page that says nothing about the real problem.
func TestUnconfiguredProvidersAreUnsupported(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{RedirectBaseURL: redirectBase})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	for _, id := range []domain.Provider{
		domain.ProviderGoogle,
		domain.ProviderGitHub,
		// Apple is deliberately not implemented yet. It has to fail the same way
		// as anything else missing rather than half-work.
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

// Half-filled credentials are worse than none: the provider looks available and
// then fails at the exchange, in the middle of someone's sign-in.
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

// The fake provider accepts any identity presented to it. In production that is
// not a testing convenience, it is an authentication bypass — so the service
// refuses to start rather than logging a warning nobody reads.
func TestTheFakeProviderIsRefusedInProduction(t *testing.T) {
	_, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  redirectBase,
		FakeEnabled:      true,
		FakeAuthorizeURL: "http://auth:8081" + oauth.FakeAuthorizePath,
		Production:       true,
	})
	if err == nil {
		t.Fatal("the fake provider was accepted in production")
	}
	if !strings.Contains(err.Error(), "production") {
		t.Errorf("the error does not say why: %v", err)
	}
}

func TestTheFakeProviderIsAvailableOutsideProduction(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  redirectBase,
		FakeEnabled:      true,
		FakeAuthorizeURL: "http://auth:8081" + oauth.FakeAuthorizePath,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if _, err := registry.Get(domain.ProviderFake); err != nil {
		t.Fatalf("Get(fake): %v", err)
	}
}

func TestTheFakeProviderNeedsItsAuthorizeURL(t *testing.T) {
	_, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL: redirectBase,
		FakeEnabled:     true,
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

// The order is fixed rather than map order: a sign-in page whose buttons
// reshuffle on every restart is a bug nobody files.
func TestAvailableIsInAStableOrder(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		RedirectBaseURL:  redirectBase,
		Google:           configured(),
		GitHub:           configured(),
		FakeEnabled:      true,
		FakeAuthorizeURL: "http://auth:8081" + oauth.FakeAuthorizePath,
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

// Each provider gets its own callback route, so the client knows which flow it
// is completing without being told separately.
func TestEachProviderRedirectsToItsOwnRoute(t *testing.T) {
	registry, err := oauth.NewRegistry(oauth.RegistryConfig{
		// A trailing slash in configuration must not produce a doubled one.
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

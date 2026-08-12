package oauth_test

import (
	"errors"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

const webOrigin = "http://localhost:3000"

func newPolicy(t *testing.T, origins ...string) *oauth.ReturnToPolicy {
	t.Helper()

	if len(origins) == 0 {
		origins = []string{webOrigin}
	}

	policy, err := oauth.NewReturnToPolicy(origins)
	if err != nil {
		t.Fatalf("NewReturnToPolicy: %v", err)
	}
	return policy
}

func TestAllowedDestinationsPassThrough(t *testing.T) {
	policy := newPolicy(t)

	for _, target := range []string{
		"http://localhost:3000",
		"http://localhost:3000/lobby",
		"http://localhost:3000/games/chess?rematch=1",
		"http://localhost:3000/x#fragment",
	} {
		got, err := policy.Resolve(target)
		if err != nil {
			t.Errorf("Resolve(%q): %v", target, err)
			continue
		}
		if got != target {
			t.Errorf("Resolve(%q) = %q; the destination must not be rewritten", target, got)
		}
	}
}

func TestDestinationsOutsideTheAllowListAreRefused(t *testing.T) {
	policy := newPolicy(t)

	for _, target := range []string{
		"https://evil.example/collect",

		"http://localhost:3000.evil.example",
		"http://evil.example/?next=http://localhost:3000",

		"https://localhost:3000",
		"http://localhost:30001",

		"//evil.example/collect",

		"/lobby",
		"javascript:alert(1)",
	} {
		if _, err := policy.Resolve(target); !errors.Is(err, domain.ErrOauthReturnToNotAllowed) {
			t.Errorf("Resolve(%q) = %v, want ErrOauthReturnToNotAllowed", target, err)
		}
	}
}

func TestNoDestinationResolvesToTheFallback(t *testing.T) {
	policy := newPolicy(t)

	for _, empty := range []string{"", "   "} {
		got, err := policy.Resolve(empty)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", empty, err)
		}
		if got != webOrigin {
			t.Errorf("Resolve(%q) = %q, want the fallback %q", empty, got, webOrigin)
		}
	}
}

func TestHostAndSchemeAreComparedCaseInsensitively(t *testing.T) {
	policy := newPolicy(t, "https://Axon.Example")

	if _, err := policy.Resolve("HTTPS://AXON.EXAMPLE/lobby"); err != nil {
		t.Errorf("a case difference in the host refused an allowed origin: %v", err)
	}
}

func TestSeveralOriginsAreAllowed(t *testing.T) {
	origins := []string{"http://localhost:3000", "https://axon.example"}
	policy := newPolicy(t, origins...)

	for _, origin := range origins {
		if _, err := policy.Resolve(origin + "/lobby"); err != nil {
			t.Errorf("Resolve for %q: %v", origin, err)
		}
	}
}

func TestAnEmptyAllowListIsRefused(t *testing.T) {
	for _, origins := range [][]string{nil, {}, {""}, {"not a url"}} {
		if _, err := oauth.NewReturnToPolicy(origins); err == nil {
			t.Errorf("NewReturnToPolicy(%q) was accepted", origins)
		}
	}
}

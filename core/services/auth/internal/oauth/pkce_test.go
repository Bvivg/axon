package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/auth/internal/oauth"
)

func TestVerifiersAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)

	for range 100 {
		verifier, err := oauth.NewVerifier()
		if err != nil {
			t.Fatalf("NewVerifier: %v", err)
		}
		if _, repeat := seen[verifier]; repeat {
			t.Fatal("the same verifier was generated twice")
		}
		seen[verifier] = struct{}{}
	}
}

func TestVerifierIsWellFormed(t *testing.T) {
	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("length = %d, want between 43 and 128", len(verifier))
	}

	for _, r := range verifier {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			t.Errorf("verifier contains %q, which is not unreserved", r)
		}
	}
}

func TestChallengeIsTheHashNotTheVerifier(t *testing.T) {
	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	challenge := oauth.Challenge(verifier)

	if challenge == verifier {
		t.Fatal("the challenge is the verifier: this is plain, not S256")
	}

	sum := sha256.Sum256([]byte(verifier))
	if want := base64.RawURLEncoding.EncodeToString(sum[:]); challenge != want {
		t.Errorf("challenge = %q, want the base64url SHA-256 %q", challenge, want)
	}
}

func TestChallengeIsDeterministic(t *testing.T) {
	const verifier = "a-fixed-verifier-value-for-this-test-only-x"

	if first, second := oauth.Challenge(verifier), oauth.Challenge(verifier); first != second {
		t.Errorf("the same verifier produced %q and %q", first, second)
	}
}

func TestOnlyTheMatchingVerifierPassesItsChallenge(t *testing.T) {
	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	other, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	challenge := oauth.Challenge(verifier)

	tests := []struct {
		name      string
		verifier  string
		challenge string
		want      bool
	}{
		{"matching", verifier, challenge, true},
		{"another verifier", other, challenge, false},
		{"empty verifier", "", challenge, false},
		{"the challenge itself", challenge, challenge, false},
		{"short verifier", verifier[:20], oauth.Challenge(verifier[:20]), false},
		{"malformed challenge", verifier, "not a challenge", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oauth.VerifierMatches(tt.verifier, tt.challenge); got != tt.want {
				t.Errorf("VerifierMatches = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChallengesAreS256Shaped(t *testing.T) {
	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	for challenge, want := range map[string]bool{
		oauth.Challenge(verifier):     true,
		"":                            false,
		"short":                       false,
		strings.Repeat("a", 42) + "=": false,
		strings.Repeat("a", 42) + "/": false,
		strings.Repeat("a", 44):       false,
	} {
		if got := oauth.ValidChallenge(challenge); got != want {
			t.Errorf("ValidChallenge(%q) = %v, want %v", challenge, got, want)
		}
	}
}

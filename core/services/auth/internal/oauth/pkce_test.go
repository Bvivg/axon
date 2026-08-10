package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
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

// RFC 7636 requires 43 to 128 characters, unreserved. Base64url of 32 bytes is
// 43 with no padding, which is both the minimum length and 256 bits of entropy.
func TestVerifierIsWellFormed(t *testing.T) {
	verifier, err := oauth.NewVerifier()
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("length = %d, want between 43 and 128", len(verifier))
	}

	// Padding would need percent-encoding in a query string, and some providers
	// reject it outright.
	for _, r := range verifier {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-', r == '_':
		default:
			t.Errorf("verifier contains %q, which is not unreserved", r)
		}
	}
}

// The whole point of S256 is that the challenge does not reveal the verifier.
// A challenge equal to its verifier would be "plain" by another name.
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

package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// Method is the only PKCE transform this service uses.
//
// The specification also allows "plain", where the challenge is the verifier
// itself. That defends against nothing — anyone who can read the challenge off
// the authorization request can replay it — so it is not offered here.
const Method = "S256"

// verifierBytes is the entropy behind a verifier. RFC 7636 permits 43 to 128
// characters after encoding; 32 random bytes lands at 43 and carries 256 bits,
// which is the part that matters.
const verifierBytes = 32

// NewVerifier returns a fresh PKCE code verifier.
//
// The verifier never leaves this service until the token exchange, and the
// authorization request carries only its hash. An attacker who intercepts the
// authorization code therefore cannot redeem it: the exchange needs a value
// they never saw.
func NewVerifier() (string, error) {
	raw := make([]byte, verifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Challenge derives the S256 challenge sent on the authorization request.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

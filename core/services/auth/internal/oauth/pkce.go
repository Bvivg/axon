package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
)

const Method = "S256"

const verifierBytes = 32

func NewVerifier() (string, error) {
	raw := make([]byte, verifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("oauth: generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const challengeLength = 43

func ValidChallenge(challenge string) bool {
	if len(challenge) != challengeLength {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil
}

func VerifierMatches(verifier, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 || !ValidChallenge(challenge) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(Challenge(verifier)), []byte(challenge)) == 1
}

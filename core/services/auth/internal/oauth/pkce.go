package oauth

import (
	"crypto/rand"
	"crypto/sha256"
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

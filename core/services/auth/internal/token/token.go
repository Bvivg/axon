package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
)

const Bytes = 32

type Token struct {
	Value string

	Hash string
}

var ErrEmpty = errors.New("token: value is empty")

func New() (Token, error) {
	buf := make([]byte, Bytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, fmt.Errorf("token: read random bytes: %w", err)
	}

	value := base64.RawURLEncoding.EncodeToString(buf)

	return Token{Value: value, Hash: hash(value)}, nil
}

func Hash(value string) (string, error) {
	if value == "" {
		return "", ErrEmpty
	}
	return hash(value), nil
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

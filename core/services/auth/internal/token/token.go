// Package token generates opaque refresh tokens and the hashes they are stored
// as.
//
// A refresh token is not a JWT. It carries no claims and means nothing on its
// own: it is a random handle that only has value because a row in the database
// points at it. That is the whole reason refresh is stateful while access is not
// — a handle can be revoked, and a signed claim cannot.
//
// Hashing here is SHA-256, not argon2id, and the difference is not an oversight.
// A password is low-entropy and guessable, so verifying it has to be made
// deliberately slow. A token from this package is 256 uniformly random bits;
// there is nothing to guess, and no amount of hashing cost would change that.
// Paying argon2's memory cost on every refresh would buy nothing and slow down
// the one endpoint every client hits on a schedule.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
)

// Bytes is the entropy of a generated token. 32 bytes is the width of the hash
// it is stored under, and well past any brute-force concern.
const Bytes = 32

// Token is a freshly generated refresh token.
type Token struct {
	// Value is what the client receives. It is never stored, and never logged.
	Value string
	// Hash is what goes in the database.
	Hash string
}

// ErrEmpty is returned when hashing an empty value, which always indicates a
// caller bug rather than a bad token.
var ErrEmpty = errors.New("token: value is empty")

// New generates a token and the hash to store it under.
func New() (Token, error) {
	buf := make([]byte, Bytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, fmt.Errorf("token: read random bytes: %w", err)
	}

	// URL-safe so the value survives being put in a header, a cookie or a JSON
	// body without any further encoding.
	value := base64.RawURLEncoding.EncodeToString(buf)

	return Token{Value: value, Hash: hash(value)}, nil
}

// Hash returns the stored form of a presented token value.
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

// Equal compares two stored hashes in constant time.
//
// The database lookup is by hash and so is already an equality test, but any
// place that compares hashes in Go should not short-circuit on the first
// differing byte.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

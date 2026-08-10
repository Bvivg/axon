package domain

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Password policy.
//
// The minimum follows current guidance: length carries far more entropy than
// forced character classes, which mostly push people towards "Password1!".
//
// The maximum exists for a different reason. argon2id cost grows with input, so
// an unbounded password field is a cheap way to make the server do expensive
// work; 128 bytes is well past any real passphrase.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

// MaxEmailLength bounds the address at the longest an SMTP path may be.
const MaxEmailLength = 254

// MaxDisplayNameLength bounds a display name.
const MaxDisplayNameLength = 64

// NormalizeEmail returns the canonical form used for storage and lookup: the
// address trimmed and lowercased.
//
// Lowercasing the local part is technically lossy — RFC 5321 lets it be
// case-sensitive — but no mail provider in practice treats Bob@ and bob@ as
// different people, and not normalising would let one person register both and
// then be unable to tell which account they are signing in to.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail checks that email is a syntactically valid single address and
// returns its normalized form.
func ValidateEmail(email string) (string, error) {
	normalized := NormalizeEmail(email)

	if normalized == "" {
		return "", newValidationError("email", "is required")
	}
	if len(normalized) > MaxEmailLength {
		return "", newValidationError("email", "is too long")
	}

	// ParseAddress accepts a display name ("Bob <bob@example.com>"), which is not
	// something anyone should be registering with.
	addr, err := mail.ParseAddress(normalized)
	if err != nil || addr.Name != "" || addr.Address != normalized {
		return "", newValidationError("email", "is not a valid address")
	}

	return normalized, nil
}

// ValidatePassword checks a password against the length policy.
//
// Length is measured in bytes, not runes: bytes are what argon2id actually
// hashes, and what the upper bound is protecting against.
func ValidatePassword(password string) error {
	switch {
	case password == "":
		return newValidationError("password", "is required")
	case len(password) < MinPasswordLength:
		return newValidationError("password", "is too short")
	case len(password) > MaxPasswordLength:
		return newValidationError("password", "is too long")
	case !utf8.ValidString(password):
		return newValidationError("password", "is not valid UTF-8")
	}
	return nil
}

// ValidateDisplayName checks an optional display name and returns it trimmed.
func ValidateDisplayName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", nil
	}
	if utf8.RuneCountInString(trimmed) > MaxDisplayNameLength {
		return "", newValidationError("display_name", "is too long")
	}
	if !utf8.ValidString(trimmed) {
		return "", newValidationError("display_name", "is not valid UTF-8")
	}
	return trimmed, nil
}

package domain

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

const MaxEmailLength = 254

const MaxDisplayNameLength = 64

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func ValidateEmail(email string) (string, error) {
	normalized := NormalizeEmail(email)

	if normalized == "" {
		return "", newValidationError("email", "is required")
	}
	if len(normalized) > MaxEmailLength {
		return "", newValidationError("email", "is too long")
	}

	addr, err := mail.ParseAddress(normalized)
	if err != nil || addr.Name != "" || addr.Address != normalized {
		return "", newValidationError("email", "is not a valid address")
	}

	return normalized, nil
}

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

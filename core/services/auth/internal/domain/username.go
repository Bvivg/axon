package domain

import (
	"strings"
	"unicode/utf8"
)

const (
	MinUsernameLength = 3
	MaxUsernameLength = 32

	usernameStemLength = 25
	fallbackUsername   = "user"
)

var reservedUsernames = map[string]struct{}{
	"admin":         {},
	"administrator": {},
	"axon":          {},
	"moderator":     {},
	"official":      {},
	"root":          {},
	"security":      {},
	"support":       {},
	"system":        {},
}

func NormalizeUsername(raw string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
}

func ValidateUsername(raw string) (string, error) {
	name := NormalizeUsername(raw)
	switch {
	case name == "":
		return "", newValidationError("username", "is required")
	case !onlyUsernameRunes(name):
		return "", newValidationError("username", "may only contain latin letters, digits and underscores")
	case !isLatinLetter(rune(name[0])):
		return "", newValidationError("username", "must start with a letter")
	case utf8.RuneCountInString(name) < MinUsernameLength:
		return "", newValidationError("username", "is too short")
	case utf8.RuneCountInString(name) > MaxUsernameLength:
		return "", newValidationError("username", "is too long")
	case IsReservedUsername(name):
		return "", newValidationError("username", "is reserved")
	}
	return name, nil
}

func IsReservedUsername(name string) bool {
	_, reserved := reservedUsernames[name]
	return reserved
}

func UsernameStem(email string) string {
	local, _, _ := strings.Cut(strings.ToLower(email), "@")

	var b strings.Builder
	separated := false
	for _, r := range local {
		switch {
		case isLatinLetter(r), isDigit(r) && b.Len() > 0:
			if separated && b.Len() > 0 {
				b.WriteByte('_')
			}
			separated = false
			b.WriteRune(r)
		default:
			separated = true
		}
	}

	stem := trimUsername(b.String(), MaxUsernameLength)
	if len(stem) < MinUsernameLength {
		return fallbackUsername
	}
	return stem
}

func UsernameWithSuffix(stem, suffix string) string {
	return trimUsername(stem, usernameStemLength) + "_" + suffix
}

func trimUsername(name string, limit int) string {
	if len(name) > limit {
		name = name[:limit]
	}
	return strings.TrimRight(name, "_")
}

func onlyUsernameRunes(name string) bool {
	for _, r := range name {
		if !isLatinLetter(r) && !isDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func isLatinLetter(r rune) bool { return r >= 'a' && r <= 'z' }

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

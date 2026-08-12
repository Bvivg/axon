package domain

import (
	"strings"
	"unicode/utf8"
)

const (
	MaxRoomNameLength = 120
	MaxMessageLength  = 4096

	MaxClientIDLength = 64

	MaxPageSize     = 200
	DefaultPageSize = 50
)

func ValidateRoomName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)

	switch {
	case trimmed == "":
		return "", newValidationError("name", "is required")
	case utf8.RuneCountInString(trimmed) > MaxRoomNameLength:
		return "", newValidationError("name", "is too long")
	case !utf8.ValidString(trimmed):
		return "", newValidationError("name", "is not valid UTF-8")
	}

	return trimmed, nil
}

func ValidateMessageBody(body string) (string, error) {
	trimmed := strings.TrimSpace(body)

	switch {
	case trimmed == "":
		return "", newValidationError("body", "is required")
	case utf8.RuneCountInString(trimmed) > MaxMessageLength:
		return "", newValidationError("body", "is too long")
	case !utf8.ValidString(trimmed):
		return "", newValidationError("body", "is not valid UTF-8")
	}

	return trimmed, nil
}

func ValidateClientID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)

	switch {
	case trimmed == "":
		return "", nil
	case len(trimmed) > MaxClientIDLength:
		return "", newValidationError("client_id", "is too long")
	case !utf8.ValidString(trimmed):
		return "", newValidationError("client_id", "is not valid UTF-8")
	}

	return trimmed, nil
}

func ValidateDisplayName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || !utf8.ValidString(trimmed) {
		return ""
	}
	if utf8.RuneCountInString(trimmed) > MaxRoomNameLength {
		return string([]rune(trimmed)[:MaxRoomNameLength])
	}
	return trimmed
}

func ValidatePage(p Page) (Page, error) {
	if p.BeforeSeq != 0 && p.AfterSeq != 0 {
		return Page{}, newValidationError("page", "cannot be bounded in both directions")
	}
	if p.BeforeSeq < 0 || p.AfterSeq < 0 {
		return Page{}, newValidationError("page", "cursor cannot be negative")
	}

	switch {
	case p.Limit <= 0:
		p.Limit = DefaultPageSize
	case p.Limit > MaxPageSize:
		p.Limit = MaxPageSize
	}

	return p, nil
}

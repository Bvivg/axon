package domain

import (
	"strings"
	"unicode/utf8"
)

// Input limits.
//
// The message bound is the interesting one: it is also written into a CHECK
// constraint on the messages table, so a bug here cannot put a row in the
// database that this package would refuse to produce. 4 KiB is far more than
// anybody types and far less than anybody should be able to broadcast to every
// socket in a room.
const (
	MaxRoomNameLength = 120
	MaxMessageLength  = 4096

	// MaxClientIDLength bounds the sender's own id for a message. It is opaque
	// here — a uuid in practice — and only ever compared, never parsed.
	MaxClientIDLength = 64

	// MaxPageSize caps a page of history, and DefaultPageSize is what a request
	// that names no limit gets. A client asking for everything gets a page.
	MaxPageSize     = 200
	DefaultPageSize = 50
)

// ValidateRoomName checks a room name and returns it trimmed.
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

// ValidateMessageBody checks a message and returns it trimmed.
//
// Trailing whitespace is cut rather than preserved: it is invisible, it makes
// two identical-looking messages differ, and nobody types it on purpose.
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

// ValidateClientID checks the optional id a sender attaches to a message.
//
// An absent id is fine — it costs the sender its own delivery echo and the
// protection against a resend arriving twice, and nothing else.
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

// ValidateDisplayName checks the name snapshot taken when somebody joins.
//
// It is not the caller's own input — it comes from auth — so an unusable value
// is dropped rather than refused: a rename should never be able to keep
// somebody out of a room.
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

// ValidatePage clamps a requested page to something the database is willing to
// answer, and refuses a request that describes no page at all.
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

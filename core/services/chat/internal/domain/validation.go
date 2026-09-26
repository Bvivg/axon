package domain

import (
	"encoding/json"
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

func ValidateCaption(body string) (string, error) {
	trimmed := strings.TrimSpace(body)

	switch {
	case utf8.RuneCountInString(trimmed) > MaxMessageLength:
		return "", newValidationError("body", "is too long")
	case !utf8.ValidString(trimmed):
		return "", newValidationError("body", "is not valid UTF-8")
	}

	return trimmed, nil
}

func ValidateMessageKind(raw string) (MessageKind, error) {
	if raw == "" {
		return MessageKindText, nil
	}

	kind := MessageKind(raw)
	if !kind.Valid() {
		return "", newValidationError("kind", "is not a known message kind")
	}
	return kind, nil
}

func ValidatePayload(kind MessageKind, raw json.RawMessage) (json.RawMessage, error) {
	switch kind {
	case MessageKindText:
		return emptyPayload, nil

	case MessageKindVoice:
		var p VoicePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid voice payload")
		}
		if p.DurationMS <= 0 || strings.TrimSpace(p.URL) == "" {
			return nil, newValidationError("payload", "is missing duration_ms or url")
		}
		return json.Marshal(p)

	case MessageKindAttachment:
		var p AttachmentPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid attachment payload")
		}
		if strings.TrimSpace(p.URL) == "" || strings.TrimSpace(p.Filename) == "" ||
			strings.TrimSpace(p.Mime) == "" || p.SizeBytes <= 0 {
			return nil, newValidationError("payload", "is missing url, filename, mime, or size_bytes")
		}
		return json.Marshal(p)

	case MessageKindSystem:
		var p SystemPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid system payload")
		}
		if strings.TrimSpace(p.Event) == "" {
			return nil, newValidationError("payload", "is missing event")
		}
		return json.Marshal(p)

	default:
		return nil, newValidationError("kind", "is not a known message kind")
	}
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

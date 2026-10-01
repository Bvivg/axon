package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	MaxRoomNameLength = 120
	MaxMessageLength  = 4096

	MaxClientIDLength = 64

	MaxPageSize     = 200
	DefaultPageSize = 50

	MaxGroupMembers = 100

	MaxForwardMessages = 100
	MaxForwardTargets  = 50

	MaxDeleteMessages = 100

	MinAlbumItems = 2
	MaxAlbumItems = 10

	MinSearchQueryLength = 2
	MaxSearchQueryLength = 100
	MaxSearchResults     = 50
)

func ValidateUserIDs(field string, raw []string, exclude uuid.UUID) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]struct{}, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))

	for _, value := range raw {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, newValidationError(field, "contains an invalid id")
		}
		if id == exclude {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) >= MaxGroupMembers {
		return nil, newValidationError(field, "has too many people")
	}

	return ids, nil
}

func ValidateIDList(field string, raw []string, limit int) ([]uuid.UUID, error) {
	seen := make(map[uuid.UUID]struct{}, len(raw))
	ids := make([]uuid.UUID, 0, len(raw))

	for _, value := range raw {
		id, err := uuid.Parse(strings.TrimSpace(value))
		if err != nil {
			return nil, newValidationError(field, "contains an invalid id")
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	switch {
	case len(ids) == 0:
		return nil, newValidationError(field, "is required")
	case len(ids) > limit:
		return nil, newValidationError(field, "has too many ids")
	}

	return ids, nil
}

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

func ValidateSearchQuery(query string) (string, error) {
	trimmed := strings.TrimSpace(query)

	switch {
	case !utf8.ValidString(trimmed):
		return "", newValidationError("query", "is not valid UTF-8")
	case utf8.RuneCountInString(trimmed) < MinSearchQueryLength:
		return "", newValidationError("query", "is too short")
	case utf8.RuneCountInString(trimmed) > MaxSearchQueryLength:
		return "", newValidationError("query", "is too long")
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

	case MessageKindImage:
		var p ImagePayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid image payload")
		}
		if strings.TrimSpace(p.URL) == "" || p.Width <= 0 || p.Height <= 0 || p.SizeBytes <= 0 {
			return nil, newValidationError("payload", "is missing url, width, height, or size_bytes")
		}
		return json.Marshal(p)

	case MessageKindVideo:
		var p VideoPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid video payload")
		}
		if strings.TrimSpace(p.URL) == "" || p.Width <= 0 || p.Height <= 0 || p.SizeBytes <= 0 {
			return nil, newValidationError("payload", "is missing url, width, height, or size_bytes")
		}
		return json.Marshal(p)

	case MessageKindAlbum:
		var p AlbumPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, newValidationError("payload", "is not a valid album payload")
		}
		if len(p.Items) < MinAlbumItems || len(p.Items) > MaxAlbumItems {
			return nil, newValidationError("payload", "an album holds 2 to 10 photos or videos")
		}
		for _, item := range p.Items {
			if item.Kind != MessageKindImage && item.Kind != MessageKindVideo {
				return nil, newValidationError("payload", "an album holds only photos and videos")
			}
			if strings.TrimSpace(item.URL) == "" || item.Width <= 0 || item.Height <= 0 || item.SizeBytes <= 0 {
				return nil, newValidationError("payload", "an album item is missing url, width, height, or size_bytes")
			}
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

	for _, kind := range p.Kinds {
		if !kind.Valid() {
			return Page{}, newValidationError("kinds", "has an unknown message kind")
		}
	}

	return p, nil
}

func AlbumItemOf(kind MessageKind, payload json.RawMessage) (AlbumItem, error) {
	if kind != MessageKindImage && kind != MessageKindVideo {
		return AlbumItem{}, newValidationError("upload_ids", "an album holds only photos and videos")
	}
	var item AlbumItem
	if err := json.Unmarshal(payload, &item); err != nil {
		return AlbumItem{}, newValidationError("upload_ids", "an upload has no usable payload")
	}
	item.Kind = kind
	return item, nil
}

func (i AlbumItem) Single() (MessageKind, json.RawMessage, error) {
	var payload any
	switch i.Kind {
	case MessageKindImage:
		payload = ImagePayload{URL: i.URL, ThumbnailURL: i.ThumbnailURL, Width: i.Width, Height: i.Height, SizeBytes: i.SizeBytes, Mime: i.Mime}
	case MessageKindVideo:
		payload = VideoPayload{
			URL: i.URL, PosterURL: i.PosterURL, Width: i.Width, Height: i.Height,
			DurationMS: i.DurationMS, SizeBytes: i.SizeBytes, Mime: i.Mime,
		}
	default:
		return "", nil, newValidationError("payload", "an album holds only photos and videos")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("domain: album item payload: %w", err)
	}
	return i.Kind, raw, nil
}

const (
	maxPushEndpoint = 2048
	maxPushKey      = 256
)

func ValidatePushSubscription(s PushSubscription) (PushSubscription, error) {
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	s.P256DH = strings.TrimSpace(s.P256DH)
	s.Auth = strings.TrimSpace(s.Auth)

	switch {
	case !strings.HasPrefix(s.Endpoint, "https://") || len(s.Endpoint) > maxPushEndpoint:
		return PushSubscription{}, newValidationError("endpoint", "must be an https URL")
	case s.P256DH == "" || len(s.P256DH) > maxPushKey:
		return PushSubscription{}, newValidationError("p256dh", "is missing or too long")
	case s.Auth == "" || len(s.Auth) > maxPushKey:
		return PushSubscription{}, newValidationError("auth", "is missing or too long")
	}
	return s, nil
}

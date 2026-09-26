package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type RoomKind string

const (
	RoomKindOpen   RoomKind = "open"
	RoomKindDirect RoomKind = "direct"
)

func (k RoomKind) Valid() bool {
	switch k {
	case RoomKindOpen, RoomKindDirect:
		return true
	default:
		return false
	}
}

type Room struct {
	ID        uuid.UUID
	Name      string
	CreatedBy uuid.UUID
	CreatedAt time.Time

	MemberCount   int
	UnreadCount   int64
	OthersReadSeq int64

	Kind          RoomKind
	DirectUserMin *uuid.UUID
	DirectUserMax *uuid.UUID
}

func (r Room) DirectPeerFor(userID uuid.UUID) *uuid.UUID {
	if r.Kind != RoomKindDirect || r.DirectUserMin == nil || r.DirectUserMax == nil {
		return nil
	}
	if *r.DirectUserMin == userID {
		return r.DirectUserMax
	}
	if *r.DirectUserMax == userID {
		return r.DirectUserMin
	}
	return nil
}

func DirectPair(a, b uuid.UUID) (min, max uuid.UUID) {
	if a.String() < b.String() {
		return a, b
	}
	return b, a
}

type Member struct {
	UserID   uuid.UUID
	JoinedAt time.Time

	DisplayName string

	HiddenAt          *time.Time
	ClearedThroughSeq int64

	LastReadSeq int64
}

type MessageKind string

const (
	MessageKindText       MessageKind = "text"
	MessageKindVoice      MessageKind = "voice"
	MessageKindAttachment MessageKind = "attachment"
	MessageKindImage      MessageKind = "image"
	MessageKindVideo      MessageKind = "video"
	MessageKindSystem     MessageKind = "system"
)

func (k MessageKind) Valid() bool {
	switch k {
	case MessageKindText, MessageKindVoice, MessageKindAttachment, MessageKindImage, MessageKindVideo, MessageKindSystem:
		return true
	default:
		return false
	}
}

func (k MessageKind) FromUpload() bool {
	switch k {
	case MessageKindVoice, MessageKindAttachment, MessageKindImage, MessageKindVideo:
		return true
	default:
		return false
	}
}

type VoicePayload struct {
	DurationMS int64  `json:"duration_ms"`
	URL        string `json:"url"`
	Mime       string `json:"mime,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
}

type AttachmentPayload struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

type ImagePayload struct {
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	SizeBytes    int64  `json:"size_bytes"`
	Mime         string `json:"mime"`
}

type VideoPayload struct {
	URL        string `json:"url"`
	PosterURL  string `json:"poster_url"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	DurationMS int64  `json:"duration_ms"`
	SizeBytes  int64  `json:"size_bytes"`
	Mime       string `json:"mime"`
}

type Upload struct {
	ID         uuid.UUID
	UploaderID uuid.UUID
	Kind       MessageKind
	Payload    json.RawMessage
	CreatedAt  time.Time
}

type SystemPayload struct {
	Event    string  `json:"event"`
	ActorID  *string `json:"actor_id,omitempty"`
	TargetID *string `json:"target_id,omitempty"`
}

var emptyPayload = json.RawMessage(`{}`)

type Message struct {
	ID       uuid.UUID
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Body     string

	Seq int64

	SentAt time.Time

	ClientID string

	Kind    MessageKind
	Payload json.RawMessage

	ReplyToID       *uuid.UUID
	ForwardedFromID *uuid.UUID
}

type Page struct {
	Limit     int
	BeforeSeq int64
	AfterSeq  int64

	Kinds []MessageKind
}

func (p Page) Backward() bool { return p.AfterSeq == 0 }

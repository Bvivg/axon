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
	MessageKindSystem     MessageKind = "system"
)

func (k MessageKind) Valid() bool {
	switch k {
	case MessageKindText, MessageKindVoice, MessageKindAttachment, MessageKindSystem:
		return true
	default:
		return false
	}
}

type VoicePayload struct {
	DurationMS int64  `json:"duration_ms"`
	URL        string `json:"url"`
}

type AttachmentPayload struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
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
}

func (p Page) Backward() bool { return p.AfterSeq == 0 }

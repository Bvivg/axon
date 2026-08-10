// Package domain holds the chat service's own types and rules.
//
// Nothing here knows about Postgres, Redis, Kafka or WebSockets. A room is a
// room whether it arrived over a Connect call or a socket frame, and the rules
// about who may write to one do not change with the transport.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Room is a conversation.
type Room struct {
	ID        uuid.UUID
	Name      string
	CreatedBy uuid.UUID
	CreatedAt time.Time

	// MemberCount is filled by the queries that read rooms for display. It is
	// not part of the room's identity, and a room loaded to be written to may
	// leave it zero.
	MemberCount int
}

// Member is somebody in a room.
type Member struct {
	UserID   uuid.UUID
	JoinedAt time.Time

	// DisplayName is what the person was called when they joined.
	//
	// The chat service has no account of its own to read. This is copied once,
	// from auth, using the joining person's own token — so it can go stale, and
	// that is the trade: a name that lags a rename, against every rendered
	// message depending on another service answering.
	DisplayName string
}

// Message is one thing somebody said in a room.
type Message struct {
	ID       uuid.UUID
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Body     string

	// Seq is the message's position in its room: monotonic, allocated under the
	// room's lock, and the only thing clients page by. Timestamps are not a
	// cursor — two messages can share one.
	Seq int64

	SentAt time.Time

	// ClientID is the id the sender minted before sending, echoed back so a
	// client can match a delivery to the message it already drew. Empty when
	// the sender offered none.
	ClientID string
}

// Page bounds a read of history.
//
// BeforeSeq and AfterSeq are the two directions a client reads in: back through
// what came before, or forward from where it left off. They are mutually
// exclusive — a request carrying both describes no page — and both being zero
// means "the most recent page".
type Page struct {
	Limit     int
	BeforeSeq int64
	AfterSeq  int64
}

// Backward reports whether the page reads into the past. The most recent page
// counts: it is the first step of paging backwards.
func (p Page) Backward() bool { return p.AfterSeq == 0 }

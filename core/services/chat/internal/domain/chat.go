package domain

import (
	"time"

	"github.com/google/uuid"
)

type Room struct {
	ID        uuid.UUID
	Name      string
	CreatedBy uuid.UUID
	CreatedAt time.Time

	MemberCount int
}

type Member struct {
	UserID   uuid.UUID
	JoinedAt time.Time

	DisplayName string
}

type Message struct {
	ID       uuid.UUID
	RoomID   uuid.UUID
	AuthorID uuid.UUID
	Body     string

	Seq int64

	SentAt time.Time

	ClientID string
}

type Page struct {
	Limit     int
	BeforeSeq int64
	AfterSeq  int64
}

func (p Page) Backward() bool { return p.AfterSeq == 0 }

package domain

import (
	"time"

	"github.com/google/uuid"
)

type AdminRoomFilter struct {
	Query  string
	Kind   RoomKind
	Member *uuid.UUID
	Cursor string
	Limit  int
}

type AdminRoom struct {
	Room         Room
	LastActivity *time.Time
	LastSeq      int64
}

type AdminUploadFilter struct {
	Uploader *uuid.UUID
	Room     *uuid.UUID
	Kind     MessageKind
	Cursor   string
	Limit    int
}

type AdminUpload struct {
	ID          uuid.UUID
	UploaderID  uuid.UUID
	Kind        MessageKind
	Name        string
	ContentType string
	SizeBytes   int64
	CreatedAt   time.Time
	RoomID      *uuid.UUID
	MessageID   *uuid.UUID
}

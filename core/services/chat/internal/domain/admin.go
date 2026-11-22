package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type AdminRoomFilter struct {
	Query  string
	Kind   RoomKind
	Member *uuid.UUID
	Cursor string
	Limit  int
	List   adminquery.Request
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
	List     adminquery.Request
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

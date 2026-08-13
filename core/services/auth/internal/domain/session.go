package domain

import (
	"time"

	"github.com/google/uuid"
)

type Device struct {
	UserAgent string
	IP        string
}

type Session struct {
	FamilyID uuid.UUID

	UserAgent string
	IP        string

	StartedAt  time.Time
	LastUsedAt time.Time

	Current bool
	Online  bool
}

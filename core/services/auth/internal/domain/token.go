package domain

import (
	"time"

	"github.com/google/uuid"
)

type RefreshToken struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	FamilyID uuid.UUID

	TokenHash string

	IssuedAt  time.Time
	ExpiresAt time.Time

	UsedAt time.Time

	RevokedAt time.Time
}

func (t RefreshToken) Used() bool { return !t.UsedAt.IsZero() }

func (t RefreshToken) Revoked() bool { return !t.RevokedAt.IsZero() }

func (t RefreshToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

func (t RefreshToken) Usable(now time.Time) bool {
	return !t.Used() && !t.Revoked() && !t.Expired(now)
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string

	AccessExpiresAt time.Time
}

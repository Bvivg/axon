package domain

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken is one link in a refresh chain.
//
// The token itself is never stored — only TokenHash. A leaked database dump has
// to be useless for signing in, and a hash makes it so.
//
// FamilyID ties together every token descended from one sign-in. Refreshing
// marks the presented token used and issues a successor in the same family. If a
// token that is already used comes back, the only explanations are theft or a
// replay, and neither is safe to serve: the whole family is revoked, which
// signs out the attacker and the legitimate holder alike. Logging back in is a
// far better outcome than an attacker keeping a live session.
type RefreshToken struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	FamilyID uuid.UUID

	TokenHash string

	IssuedAt  time.Time
	ExpiresAt time.Time

	// UsedAt is set the moment the token is exchanged. Non-zero means spent.
	UsedAt time.Time

	// RevokedAt is set by logout or by reuse detection revoking the family.
	RevokedAt time.Time
}

// Used reports whether the token has already been exchanged.
func (t RefreshToken) Used() bool { return !t.UsedAt.IsZero() }

// Revoked reports whether the token has been revoked.
func (t RefreshToken) Revoked() bool { return !t.RevokedAt.IsZero() }

// Expired reports whether the token is past its lifetime at the given instant.
func (t RefreshToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

// Usable reports whether the token can be exchanged right now: unspent,
// unrevoked and unexpired.
func (t RefreshToken) Usable(now time.Time) bool {
	return !t.Used() && !t.Revoked() && !t.Expired(now)
}

// TokenPair is what a client receives after a successful sign-in or refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string

	// AccessExpiresAt is when AccessToken stops being accepted. The transport
	// layer turns it into the seconds-remaining the contract exposes, so clients
	// never parse the JWT to find out.
	AccessExpiresAt time.Time
}

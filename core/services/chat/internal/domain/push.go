package domain

import "github.com/google/uuid"

type PushSubscription struct {
	Endpoint string
	UserID   uuid.UUID
	P256DH   string
	Auth     string
}

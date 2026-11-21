package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

type SecurityAction string

const (
	SecurityLoginSucceeded      SecurityAction = "login_succeeded"
	SecurityLoginFailed         SecurityAction = "login_failed"
	SecurityLoggedOut           SecurityAction = "logged_out"
	SecurityLoggedOutEverywhere SecurityAction = "logged_out_everywhere"
	SecuritySessionRevoked      SecurityAction = "session_revoked"
	SecurityRefreshTokenReused  SecurityAction = "refresh_token_reused"
	SecurityRoleAssigned        SecurityAction = "role_assigned"
	SecurityRoleRevoked         SecurityAction = "role_revoked"
	SecurityEmailChanged        SecurityAction = "email_changed"
)

type SecurityEvent struct {
	Action SecurityAction

	UserID  uuid.UUID
	ActorID uuid.UUID
	Subject string

	Device      Device
	EmailSHA256 string

	OccurredAt time.Time
}

func EmailSHA256(email string) string {
	sum := sha256.Sum256([]byte(NormalizeEmail(email)))
	return hex.EncodeToString(sum[:])
}

type deviceKey struct{}

func ContextWithDevice(ctx context.Context, d Device) context.Context {
	return context.WithValue(ctx, deviceKey{}, d)
}

func DeviceFromContext(ctx context.Context) Device {
	d, _ := ctx.Value(deviceKey{}).(Device)
	return d
}

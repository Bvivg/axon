package repository_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

func TestSecurityAuditEvent(t *testing.T) {
	occurred := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	user, admin := uuid.New(), uuid.New()
	family, role := uuid.New(), uuid.New()
	device := domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"}

	tests := []struct {
		name          string
		event         domain.SecurityEvent
		wantAction    eventsv1.SecurityAction
		wantAggregate string
	}{
		{
			name:          "password sign-in",
			event:         domain.SecurityEvent{Action: domain.SecurityLoginSucceeded, UserID: user, ActorID: user, Subject: family.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED,
			wantAggregate: user.String(),
		},
		{
			name:          "oauth sign-in",
			event:         domain.SecurityEvent{Action: domain.SecurityLoginSucceeded, UserID: user, ActorID: user, Subject: "google"},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED,
			wantAggregate: user.String(),
		},
		{
			name:          "unknown email",
			event:         domain.SecurityEvent{Action: domain.SecurityLoginFailed, EmailSHA256: domain.EmailSHA256("ada@axon.test")},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED,
			wantAggregate: "anonymous",
		},
		{
			name:          "wrong password",
			event:         domain.SecurityEvent{Action: domain.SecurityLoginFailed, UserID: user, EmailSHA256: domain.EmailSHA256("ada@axon.test")},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED,
			wantAggregate: user.String(),
		},
		{
			name:          "logout",
			event:         domain.SecurityEvent{Action: domain.SecurityLoggedOut, UserID: user, ActorID: user, Subject: family.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT,
			wantAggregate: user.String(),
		},
		{
			name:          "logout everywhere",
			event:         domain.SecurityEvent{Action: domain.SecurityLoggedOutEverywhere, UserID: user, ActorID: user},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_LOGGED_OUT_EVERYWHERE,
			wantAggregate: user.String(),
		},
		{
			name:          "session revoked",
			event:         domain.SecurityEvent{Action: domain.SecuritySessionRevoked, UserID: user, ActorID: user, Subject: family.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_SESSION_REVOKED,
			wantAggregate: user.String(),
		},
		{
			name:          "refresh token reused",
			event:         domain.SecurityEvent{Action: domain.SecurityRefreshTokenReused, UserID: user, Subject: family.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_REFRESH_TOKEN_REUSED,
			wantAggregate: user.String(),
		},
		{
			name:          "role assigned",
			event:         domain.SecurityEvent{Action: domain.SecurityRoleAssigned, UserID: user, ActorID: admin, Subject: role.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_ROLE_ASSIGNED,
			wantAggregate: user.String(),
		},
		{
			name:          "role revoked",
			event:         domain.SecurityEvent{Action: domain.SecurityRoleRevoked, UserID: user, ActorID: admin, Subject: role.String()},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_ROLE_REVOKED,
			wantAggregate: user.String(),
		},
		{
			name:          "email changed",
			event:         domain.SecurityEvent{Action: domain.SecurityEmailChanged, UserID: user, ActorID: user, Subject: family.String(), EmailSHA256: domain.EmailSHA256("ada@axon.test")},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_EMAIL_CHANGED,
			wantAggregate: user.String(),
		},
		{
			name:          "unknown action",
			event:         domain.SecurityEvent{Action: "password_changed", UserID: user},
			wantAction:    eventsv1.SecurityAction_SECURITY_ACTION_UNSPECIFIED,
			wantAggregate: user.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.event.Device = device
			tt.event.OccurredAt = occurred
			id := uuid.New()

			event := repository.SecurityAuditEvent(id, tt.event)
			if event.ID != id || event.Topic != "auth.audit.v1" || event.AggregateID != tt.wantAggregate {
				t.Fatalf("event = %+v", event)
			}
			payload, ok := event.Payload.(*eventsv1.SecurityEvent)
			if !ok {
				t.Fatalf("payload is %T", event.Payload)
			}

			want := &eventsv1.SecurityEvent{
				EventId:     id.String(),
				Action:      tt.wantAction,
				UserId:      idOrEmpty(tt.event.UserID),
				ActorId:     idOrEmpty(tt.event.ActorID),
				Subject:     tt.event.Subject,
				ClientIp:    "203.0.113.7",
				UserAgent:   "Firefox/140",
				EmailSha256: tt.event.EmailSHA256,
				OccurredAt:  payload.GetOccurredAt(),
			}
			if !proto.Equal(payload, want) {
				t.Fatalf("payload = %v\nwant %v", payload, want)
			}
			if !payload.GetOccurredAt().AsTime().Equal(occurred) {
				t.Fatalf("occurred_at = %v, want %v", payload.GetOccurredAt().AsTime(), occurred)
			}

			wire, err := proto.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(wire, []byte("ada@axon.test")) {
				t.Fatal("the serialized event carries the raw email address")
			}
		})
	}
}

func idOrEmpty(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

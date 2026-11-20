package repository_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

func TestUserRegisteredEventIsKeyedByTheUser(t *testing.T) {
	registered := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for name, user := range map[string]domain.User{
		"password sign-up": {ID: uuid.New(), Username: "ada", Email: "ada@axon.test", CreatedAt: registered},
		"oauth sign-up":    {ID: uuid.New(), Username: "grace_h", EmailVerified: true, CreatedAt: registered.Add(time.Hour)},
	} {
		t.Run(name, func(t *testing.T) {
			id := uuid.New()
			event := repository.UserRegisteredEvent(id, user)
			if event.ID != id || event.Topic != "auth.user.registered.v1" || event.AggregateID != user.ID.String() {
				t.Fatalf("event = %+v", event)
			}
			payload, ok := event.Payload.(*eventsv1.UserRegistered)
			if !ok {
				t.Fatalf("payload is %T", event.Payload)
			}
			if payload.GetEventId() != id.String() || payload.GetUserId() != user.ID.String() || payload.GetUsername() != user.Username {
				t.Fatalf("payload = %v", payload)
			}
			if !payload.GetRegisteredAt().AsTime().Equal(user.CreatedAt) {
				t.Fatalf("registered_at = %v, want %v", payload.GetRegisteredAt().AsTime(), user.CreatedAt)
			}
		})
	}
}

func TestOutboxMigrationMatchesTheSharedSchema(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/000006_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(migration)), outbox.Migration("auth"); got != want {
		t.Fatalf("migration drifted from outbox.Migration:\n%s\nwant:\n%s", got, want)
	}
}

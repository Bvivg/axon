//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

const testHash = "$argon2id$v=19$m=65536,t=3,p=2$c29tZXNhbHQ$aGFzaA"

type outboxRow struct {
	aggregateType, aggregateID, eventType string
	payload                               []byte
}

func registrations() map[string]func(*testing.T, *repository.Repository, domain.User) (domain.User, error) {
	return map[string]func(*testing.T, *repository.Repository, domain.User) (domain.User, error){
		"password": func(t *testing.T, repo *repository.Repository, u domain.User) (domain.User, error) {
			return repo.CreateUserWithPassword(t.Context(), u, testHash)
		},
		"oauth": func(t *testing.T, repo *repository.Repository, u domain.User) (domain.User, error) {
			return repo.CreateUserWithOauthAccount(t.Context(), u, domain.OauthAccount{
				Provider:       domain.ProviderGitHub,
				ProviderUserID: "github-" + uuid.NewString(),
				Email:          u.Email,
			})
		},
	}
}

func TestRegistrationWritesUserRegisteredToTheOutbox(t *testing.T) {
	for name, register := range registrations() {
		t.Run(name, func(t *testing.T) {
			repo := newRepo(t)

			created, err := register(t, repo, domain.User{ID: uuid.New(), Username: newUsername(), Email: "outbox-" + uuid.NewString() + "@axon.test"})
			if err != nil {
				t.Fatalf("register: %v", err)
			}

			rows := outboxRows(t)
			if len(rows) != 1 {
				t.Fatalf("%d outbox rows, want 1", len(rows))
			}
			row := rows[0]
			if row.aggregateType != "auth.user.registered.v1" || row.aggregateID != created.ID.String() || row.eventType != "axon.events.v1.UserRegistered" {
				t.Fatalf("row = %+v", row)
			}
			var event eventsv1.UserRegistered
			if err := proto.Unmarshal(row.payload, &event); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if event.GetUserId() != created.ID.String() || event.GetUsername() != created.Username || event.GetEventId() == "" {
				t.Fatalf("payload = %v", &event)
			}
			if !event.GetRegisteredAt().AsTime().Equal(created.CreatedAt) {
				t.Fatalf("registered_at = %v, want %v", event.GetRegisteredAt().AsTime(), created.CreatedAt)
			}
		})
	}
}

func TestRegistrationAndItsOutboxRowRollBackTogether(t *testing.T) {
	for name, register := range registrations() {
		t.Run(name+" rejected by the users table", func(t *testing.T) {
			repo := newRepo(t)
			first := createUser(t, repo)
			before := len(outboxRows(t))

			_, err := register(t, repo, domain.User{ID: uuid.New(), Username: newUsername(), Email: first.Email})
			if !errors.Is(err, domain.ErrEmailTaken) {
				t.Fatalf("err = %v, want ErrEmailTaken", err)
			}
			if after := len(outboxRows(t)); after != before {
				t.Fatalf("outbox rows %d -> %d after a rejected registration", before, after)
			}
		})

		t.Run(name+" rejected by the outbox", func(t *testing.T) {
			repo := newRepo(t)
			if _, err := pool.Exec(t.Context(), `ALTER TABLE outbox ADD CONSTRAINT outbox_rejects_registrations CHECK (aggregatetype <> 'auth.user.registered.v1') NOT VALID`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), `ALTER TABLE outbox DROP CONSTRAINT outbox_rejects_registrations`); err != nil {
					t.Error(err)
				}
			})

			email := "outbox-" + uuid.NewString() + "@axon.test"
			if _, err := register(t, repo, domain.User{ID: uuid.New(), Username: newUsername(), Email: email}); err == nil {
				t.Fatal("registration succeeded although its event could not be recorded")
			}
			if n := countUsers(t, email); n != 0 {
				t.Fatalf("%d users kept without their registration event", n)
			}
		})
	}
}

func outboxRows(t *testing.T) []outboxRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT aggregatetype, aggregateid, type, payload FROM outbox ORDER BY created_at`)
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	defer rows.Close()

	var result []outboxRow
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.aggregateType, &row.aggregateID, &row.eventType, &row.payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("outbox: %v", err)
	}
	return result
}

//go:build integration

package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/jwt"
	"github.com/bvivg/axon/core/services/auth/internal/password"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type auditRow struct {
	id                     uuid.UUID
	aggregateID, eventType string
	payload                []byte
	event                  *eventsv1.SecurityEvent
}

func TestLoginsAndRoleChangesWriteSecurityEventsToTheOutbox(t *testing.T) {
	repo := newRepo(t)
	svc := newAuditedService(t, repo)
	device := domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"}
	ctx := domain.ContextWithDevice(t.Context(), device)

	const secret = "correct horse battery staple"
	email := "audit-" + uuid.NewString() + "@axon.test"
	registered, err := svc.Register(ctx, service.RegisterInput{Email: email, Password: secret, DisplayName: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	user := registered.User.ID

	admin := createUser(t, repo)
	if err := repo.AssignRole(t.Context(), admin.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("grant admin: %v", err)
	}

	unknown := "nobody-" + uuid.NewString() + "@axon.test"
	if _, err := svc.Login(ctx, service.LoginInput{Email: unknown, Password: secret}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("unknown email: %v", err)
	}
	if _, err := svc.Login(ctx, service.LoginInput{Email: email, Password: "not the password at all"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	signedIn, err := svc.Login(ctx, service.LoginInput{Email: email, Password: secret, Device: device})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := svc.AssignRole(ctx, admin.ID, user, domain.RoleAdmin, ""); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	sessions, err := repo.Sessions(t.Context(), user, time.Now())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	families := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		families[s.FamilyID.String()] = true
	}

	want := []struct {
		action      eventsv1.SecurityAction
		aggregateID string
		userID      string
		actorID     string
		emailSHA256 string
	}{
		{eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED, "anonymous", "", "", domain.EmailSHA256(unknown)},
		{eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED, user.String(), user.String(), "", domain.EmailSHA256(email)},
		{eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED, user.String(), user.String(), user.String(), ""},
		{eventsv1.SecurityAction_SECURITY_ACTION_ROLE_ASSIGNED, user.String(), user.String(), admin.ID.String(), ""},
	}

	rows := auditRows(t)
	if len(rows) != len(want) {
		t.Fatalf("%d auth.audit.v1 rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		row := rows[i]
		e := row.event
		if row.eventType != "axon.events.v1.SecurityEvent" || row.aggregateID != w.aggregateID {
			t.Errorf("row %d: type %s aggregate %s, want SecurityEvent keyed %s", i, row.eventType, row.aggregateID, w.aggregateID)
		}
		if e.GetEventId() != row.id.String() || e.GetAction() != w.action || e.GetUserId() != w.userID || e.GetActorId() != w.actorID {
			t.Errorf("row %d: event = %v, want %v by %q for %q", i, e, w.action, w.actorID, w.userID)
		}
		if e.GetEmailSha256() != w.emailSHA256 {
			t.Errorf("row %d: email_sha256 = %q, want %q", i, e.GetEmailSha256(), w.emailSHA256)
		}
		if e.GetClientIp() != device.IP || e.GetUserAgent() != device.UserAgent {
			t.Errorf("row %d: device = %q %q, want %+v", i, e.GetClientIp(), e.GetUserAgent(), device)
		}
		if e.GetOccurredAt().AsTime().IsZero() {
			t.Errorf("row %d: occurred_at is not set", i)
		}
		if bytes.Contains(row.payload, []byte(email)) || bytes.Contains(row.payload, []byte(unknown)) {
			t.Errorf("row %d: payload carries a raw email address", i)
		}
	}
	if subject := rows[2].event.GetSubject(); !families[subject] {
		t.Errorf("sign-in subject %q is not one of the user's sessions %v", subject, families)
	}
	if signedIn.Tokens.RefreshToken == "" {
		t.Error("the successful sign-in issued no refresh token")
	}
	if subject := rows[3].event.GetSubject(); subject != domain.RoleAdmin.String() {
		t.Errorf("role subject = %q, want %s", subject, domain.RoleAdmin)
	}
}

func newAuditedService(t *testing.T, repo *repository.Repository) *service.Service {
	t.Helper()

	params := password.DefaultParams()
	params.Memory = 8 * 1024
	params.Time = 1
	hasher, err := password.NewHasher(params)
	if err != nil {
		t.Fatal(err)
	}

	key, err := rsa.GenerateKey(rand.Reader, jwt.MinKeySizeBits)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := jwt.NewKeySet(jwt.PrivateKey{ID: "audit-test", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := jwt.NewIssuer(jwt.IssuerConfig{Keys: keys, Issuer: "https://auth.axon.test", Audience: "axon", TTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	svc, err := service.New(repo, hasher, issuer, logger.Discard(), service.Config{RefreshTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func auditRows(t *testing.T) []auditRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT id, aggregateid, type, payload FROM outbox WHERE aggregatetype = 'auth.audit.v1' ORDER BY created_at`)
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	defer rows.Close()

	var result []auditRow
	for rows.Next() {
		var row auditRow
		if err := rows.Scan(&row.id, &row.aggregateID, &row.eventType, &row.payload); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		row.event = &eventsv1.SecurityEvent{}
		if err := proto.Unmarshal(row.payload, row.event); err != nil {
			t.Fatalf("decode security event: %v", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("outbox: %v", err)
	}
	return result
}

package auditlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/auth/internal/auditlog"
	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/repository"
)

func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	raw, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecodeAdminAction(t *testing.T) {
	at := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	actor := uuid.New()
	action := authz.Action{Service: "game", Name: "session_cancelled", Outcome: authz.Denied, ActorID: actor,
		TargetType: "game_session", TargetID: "s-1", Reason: "abuse", Details: map[string]string{"game": "chess"}, At: at}
	event := action.Event(uuid.New())

	got, err := auditlog.Decode(authz.Topic, "axon.events.v1.AdminAction", marshal(t, event.Payload))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != event.ID || got.Service != "game" || got.Outcome != domain.AuditDenied || got.ActorID == nil || *got.ActorID != actor ||
		got.TargetID != "s-1" || got.Reason != "abuse" || got.Details["game"] != "chess" || !got.OccurredAt.Equal(at) {
		t.Fatalf("decoded = %+v", got)
	}
}

func TestDecodeSecurityEvent(t *testing.T) {
	user := uuid.New()
	base := &eventsv1.SecurityEvent{EventId: uuid.NewString(), UserId: user.String(), ClientIp: "198.51.100.1", OccurredAt: timestamppb.Now()}

	login := proto.Clone(base).(*eventsv1.SecurityEvent)
	login.Action = eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_SUCCEEDED
	got, err := auditlog.Decode(repository.SecurityAuditTopic, "", marshal(t, login))
	if err != nil || got.Action != "login_succeeded" || got.TargetID != user.String() || got.ActorID == nil || *got.ActorID != user {
		t.Fatalf("login = %+v, %v", got, err)
	}

	failed := proto.Clone(base).(*eventsv1.SecurityEvent)
	failed.Action = eventsv1.SecurityAction_SECURITY_ACTION_LOGIN_FAILED
	got, err = auditlog.Decode(repository.SecurityAuditTopic, "", marshal(t, failed))
	if err != nil || got.Outcome != domain.AuditFailed || got.ActorID != nil {
		t.Fatalf("failed login = %+v, %v", got, err)
	}

	role := proto.Clone(base).(*eventsv1.SecurityEvent)
	role.Action = eventsv1.SecurityAction_SECURITY_ACTION_ROLE_ASSIGNED
	if _, err := auditlog.Decode(repository.SecurityAuditTopic, "", marshal(t, role)); !errors.Is(err, auditlog.ErrSkipped) {
		t.Fatalf("role change from auth.audit.v1 = %v, want skipped (the admin action carries it)", err)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := auditlog.Decode(authz.Topic, "", []byte{0xff, 0x01}); !errors.Is(err, auditlog.ErrInvalid) {
		t.Fatalf("garbage = %v", err)
	}
	if _, err := auditlog.Decode(authz.Topic, "", marshal(t, &eventsv1.AdminAction{EventId: "nope"})); !errors.Is(err, auditlog.ErrInvalid) {
		t.Fatalf("missing fields = %v", err)
	}
	if _, err := auditlog.Decode("game.finished.v1", "", nil); !errors.Is(err, auditlog.ErrSkipped) {
		t.Fatalf("foreign topic = %v", err)
	}
}

type memoryStore struct{ seen map[uuid.UUID]bool }

func (m *memoryStore) InsertAuditEvent(_ context.Context, e domain.AuditEvent) (bool, error) {
	if m.seen[e.ID] {
		return false, nil
	}
	m.seen[e.ID] = true
	return true, nil
}

func TestHandleIsIdempotentAndSkipsBadRecords(t *testing.T) {
	store := &memoryStore{seen: map[uuid.UUID]bool{}}
	consumer := auditlog.New(auditlog.Config{Store: store, Log: discard()})
	event := authz.Action{Service: "chat", Name: "message_deleted", Outcome: authz.Succeeded, At: time.Now()}.Event(uuid.New())
	record := &kgo.Record{Topic: authz.Topic, Value: marshal(t, event.Payload), Headers: []kgo.RecordHeader{{Key: outbox.TypeHeader, Value: []byte("axon.events.v1.AdminAction")}}}

	for range 2 {
		if err := consumer.Handle(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.seen) != 1 {
		t.Fatalf("stored %d events, want 1", len(store.seen))
	}
	if err := consumer.Handle(t.Context(), &kgo.Record{Topic: authz.Topic, Value: []byte{0xff}}); err != nil {
		t.Fatalf("an invalid record must be skipped, got %v", err)
	}
}

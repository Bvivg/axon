//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

type outboxRow struct {
	aggregateType, aggregateID, eventType string
	payload                               []byte
}

func TestAPersistedMessageIsRecordedInTheOutboxOnce(t *testing.T) {
	for name, sends := range map[string][]string{
		"one message":                   {"client-1"},
		"a resend of the same message":  {"client-1", "client-1"},
		"a message without a client id": {""},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			room, owner := newRoom(t, r, "general")

			var stored domain.Message
			for _, clientID := range sends {
				stored = say(t, r, room.ID, owner, "hello", clientID)
			}

			rows := outboxRows(t, "chat.message.v1")
			if len(rows) != 1 {
				t.Fatalf("%d chat.message.v1 rows, want 1", len(rows))
			}
			if rows[0].aggregateID != room.ID.String() || rows[0].eventType != "axon.events.v1.ChatMessageSent" {
				t.Fatalf("row = %+v", rows[0])
			}
			var event eventsv1.ChatMessageSent
			if err := proto.Unmarshal(rows[0].payload, &event); err != nil {
				t.Fatal(err)
			}
			if event.GetMessageId() != stored.ID.String() || event.GetRoomId() != room.ID.String() || event.GetSenderId() != owner.String() ||
				event.GetSeq() != stored.Seq || event.GetKind() != "text" || !event.GetSentAt().AsTime().Equal(stored.SentAt) {
				t.Fatalf("payload = %v, stored = %+v", &event, stored)
			}
		})
	}
}

func TestAMessageAndItsOutboxRowRollBackTogether(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "general")
	rejectInOutbox(t, "chat.message.v1")

	if _, _, err := r.AppendMessage(t.Context(), domain.Message{ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: "lost"}); err == nil {
		t.Fatal("the message was stored although its event could not be recorded")
	}
	var messages int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM messages WHERE room_id = $1`, room.ID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("%d messages kept without their event: %v", messages, err)
	}
}

func TestAnEndedCallIsRecordedInTheOutboxOnce(t *testing.T) {
	for name, tt := range map[string]struct {
		flow     func(t *testing.T, r *repository.Repository, a, b callPeer, id uuid.UUID)
		reason   string
		answered bool
	}{
		"declined": {
			flow: func(t *testing.T, r *repository.Repository, a, b callPeer, id uuid.UUID) {
				mustCall(t, r, b, calls.Command{Action: "decline", ID: id})
			},
			reason: "declined",
		},
		"cancelled": {
			flow: func(t *testing.T, r *repository.Repository, a, b callPeer, id uuid.UUID) {
				mustCall(t, r, a, calls.Command{Action: "cancel", ID: id})
			},
			reason: "cancelled",
		},
		"answered and hung up": {
			flow: func(t *testing.T, r *repository.Repository, a, b callPeer, id uuid.UUID) {
				mustCall(t, r, b, calls.Command{Action: "accept", ID: id})
				for _, peer := range []callPeer{a, b} {
					mustCall(t, r, peer, calls.Command{Action: "ready", ID: id, Generation: 1, Ready: true})
				}
				mustCall(t, r, a, calls.Command{Action: "end", ID: id})
			},
			reason:   "ended",
			answered: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRepo(t)
			a, b := callUser(t, r), callUser(t, r)
			room := directCallRoom(t, a, b)
			call := mustCall(t, r, a, calls.Command{Action: "start", RoomID: room, Mode: "audio"})
			if rows := outboxRows(t, "call.ended.v1"); len(rows) != 0 {
				t.Fatalf("a ringing call recorded %d call.ended.v1 rows", len(rows))
			}

			tt.flow(t, r, a, b, call.ID)
			cleaners := []callPeer{a}
			if tt.answered {
				cleaners = append(cleaners, b)
			}
			for _, peer := range cleaners {
				mustCall(t, r, peer, calls.Command{Action: "cleanup", ID: call.ID, Generation: 1})
			}

			rows := outboxRows(t, "call.ended.v1")
			if len(rows) != 1 {
				t.Fatalf("%d call.ended.v1 rows, want 1", len(rows))
			}
			if rows[0].aggregateID != call.ID.String() || rows[0].eventType != "axon.events.v1.CallEnded" {
				t.Fatalf("row = %+v", rows[0])
			}
			var event eventsv1.CallEnded
			if err := proto.Unmarshal(rows[0].payload, &event); err != nil {
				t.Fatal(err)
			}
			if event.GetCallId() != call.ID.String() || event.GetRoomId() != room.String() || event.GetInitiatorId() != a.actor.UserID.String() ||
				event.GetReason() != tt.reason || len(event.GetParticipants()) != 2 || event.GetEndedAt() == nil || (event.GetStartedAt() != nil) != tt.answered {
				t.Fatalf("payload = %v", &event)
			}
		})
	}
}

func TestACallEndAndItsOutboxRowRollBackTogether(t *testing.T) {
	r := newRepo(t)
	a, b := callUser(t, r), callUser(t, r)
	call := mustCall(t, r, a, calls.Command{Action: "start", RoomID: directCallRoom(t, a, b), Mode: "audio"})
	rejectInOutbox(t, "call.ended.v1")

	if _, err := callCommand(r, b, calls.Command{Action: "decline", ID: call.ID}); err == nil {
		t.Fatal("the call ended although its event could not be recorded")
	}
	current, err := r.GetCall(t.Context(), a.actor.UserID, call.ID)
	if err != nil || current.State != calls.Ringing {
		t.Fatalf("state = %q after a rolled back decline: %v", current.State, err)
	}
}

func mustCall(t *testing.T, r *repository.Repository, peer callPeer, cmd calls.Command) calls.Call {
	t.Helper()
	call, err := callCommand(r, peer, cmd)
	if err != nil {
		t.Fatalf("%s: %v", cmd.Action, err)
	}
	return call
}

func rejectInOutbox(t *testing.T, topic string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `ALTER TABLE outbox ADD CONSTRAINT outbox_rejects_topic CHECK (aggregatetype <> '`+topic+`') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE outbox DROP CONSTRAINT outbox_rejects_topic`); err != nil {
			t.Error(err)
		}
	})
}

func outboxRows(t *testing.T, topic string) []outboxRow {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT aggregatetype, aggregateid, type, payload FROM outbox WHERE aggregatetype = $1 ORDER BY created_at`, topic)
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

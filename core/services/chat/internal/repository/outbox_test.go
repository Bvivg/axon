package repository_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func TestMessageSentEventIsKeyedByTheRoom(t *testing.T) {
	sent := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for name, m := range map[string]domain.Message{
		"text":  {ID: uuid.New(), RoomID: uuid.New(), AuthorID: uuid.New(), Seq: 1, Kind: domain.MessageKindText, SentAt: sent},
		"album": {ID: uuid.New(), RoomID: uuid.New(), AuthorID: uuid.New(), Seq: 42, Kind: "album", SentAt: sent.Add(time.Minute)},
	} {
		t.Run(name, func(t *testing.T) {
			id := uuid.New()
			event := repository.MessageSentEvent(id, m)
			if event.ID != id || event.Topic != "chat.message.v1" || event.AggregateID != m.RoomID.String() {
				t.Fatalf("event = %+v", event)
			}
			want := &eventsv1.ChatMessageSent{
				EventId:   id.String(),
				MessageId: m.ID.String(),
				RoomId:    m.RoomID.String(),
				SenderId:  m.AuthorID.String(),
				Seq:       m.Seq,
				Kind:      string(m.Kind),
				SentAt:    timestamppb.New(m.SentAt),
			}
			if !proto.Equal(event.Payload, want) {
				t.Fatalf("payload = %v, want %v", event.Payload, want)
			}
		})
	}
}

func TestCallEndedEventIsKeyedByTheCall(t *testing.T) {
	started := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	ended := started.Add(3 * time.Minute)
	caller, peer := uuid.New(), uuid.New()
	room := uuid.New()
	for name, tt := range map[string]struct {
		call calls.Call
		want *eventsv1.CallEnded
	}{
		"answered call in a room": {
			call: calls.Call{ID: uuid.New(), RoomID: room, CallerID: caller, StartedAt: &started, EndedAt: &ended, EndReason: "ended",
				Participants: []calls.Participant{{UserID: caller}, {UserID: peer}}},
			want: &eventsv1.CallEnded{RoomId: room.String(), InitiatorId: caller.String(), Participants: []string{caller.String(), peer.String()},
				StartedAt: timestamppb.New(started), EndedAt: timestamppb.New(ended), Reason: "ended"},
		},
		"roomless call that never connected": {
			call: calls.Call{ID: uuid.New(), CallerID: caller, EndedAt: &ended, EndReason: "declined",
				Participants: []calls.Participant{{UserID: caller}, {UserID: peer}}},
			want: &eventsv1.CallEnded{InitiatorId: caller.String(), Participants: []string{caller.String(), peer.String()},
				EndedAt: timestamppb.New(ended), Reason: "declined"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			id := uuid.New()
			event := repository.CallEndedEvent(id, tt.call)
			if event.ID != id || event.Topic != "call.ended.v1" || event.AggregateID != tt.call.ID.String() {
				t.Fatalf("event = %+v", event)
			}
			tt.want.EventId = id.String()
			tt.want.CallId = tt.call.ID.String()
			if !proto.Equal(event.Payload, tt.want) {
				t.Fatalf("payload = %v, want %v", event.Payload, tt.want)
			}
		})
	}
}

func TestOutboxMigrationMatchesTheSharedSchema(t *testing.T) {
	migration, err := os.ReadFile("../../migrations/000018_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(migration)), outbox.Migration("chat"); got != want {
		t.Fatalf("migration drifted from outbox.Migration:\n%s\nwant:\n%s", got, want)
	}
}

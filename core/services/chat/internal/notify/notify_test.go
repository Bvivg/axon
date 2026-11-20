package notify

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type fakeStore struct {
	room    domain.Room
	members []domain.Member
	subs    []domain.PushSubscription
	dropped []string
}

func (f *fakeStore) RoomByID(context.Context, uuid.UUID) (domain.Room, error) { return f.room, nil }

func (f *fakeStore) Members(context.Context, uuid.UUID) ([]domain.Member, error) {
	return f.members, nil
}

func (f *fakeStore) PushSubscriptions(_ context.Context, ids []uuid.UUID) ([]domain.PushSubscription, error) {
	var out []domain.PushSubscription
	for _, s := range f.subs {
		for _, id := range ids {
			if s.UserID == id {
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (f *fakeStore) DropPushEndpoint(_ context.Context, endpoint string) error {
	f.dropped = append(f.dropped, endpoint)
	return nil
}

type fakePresence map[string]bool

func (p fakePresence) Visible(_ context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = p[id]
	}
	return out, nil
}

type sent struct {
	endpoint string
	payload  Payload
	topic    string
}

type fakeSender struct {
	mu     sync.Mutex
	sent   []sent
	status map[string]int
}

func (f *fakeSender) Send(_ context.Context, sub domain.PushSubscription, payload []byte, topic string) (int, error) {
	var p Payload
	_ = json.Unmarshal(payload, &p)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{endpoint: sub.Endpoint, payload: p, topic: topic})
	if status, ok := f.status[sub.Endpoint]; ok {
		return status, nil
	}
	return http.StatusCreated, nil
}

func setup(t *testing.T, kind domain.RoomKind, visible fakePresence) (*Notifier, *fakeStore, *fakeSender, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	author, away, here := uuid.New(), uuid.New(), uuid.New()
	room := domain.Room{ID: uuid.New(), Name: "Weekend", Kind: kind}
	store := &fakeStore{
		room: room,
		members: []domain.Member{
			{UserID: author, DisplayName: "Ada"},
			{UserID: away, DisplayName: "Ben"},
			{UserID: here, DisplayName: "Cid"},
		},
		subs: []domain.PushSubscription{
			{Endpoint: "https://push/ada", UserID: author},
			{Endpoint: "https://push/ben-phone", UserID: away},
			{Endpoint: "https://push/ben-laptop", UserID: away},
			{Endpoint: "https://push/cid", UserID: here},
		},
	}
	visible[here.String()] = true
	sender := &fakeSender{status: map[string]int{}}
	n, err := New(Config{Store: store, Presence: visible, Sender: sender, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return n, store, sender, author, away, room.ID
}

func TestOnlyPeopleWhoCannotSeeTheAppGetAPush(t *testing.T) {
	n, _, sender, author, _, roomID := setup(t, domain.RoomKindGroup, fakePresence{})

	if err := n.Notify(t.Context(), domain.Message{ID: uuid.New(), RoomID: roomID, AuthorID: author, Kind: domain.MessageKindText, Body: "dinner at 8?"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if len(sender.sent) != 2 {
		t.Fatalf("sent %d pushes, want one to each of Ben's two devices", len(sender.sent))
	}
	for _, s := range sender.sent {
		if s.endpoint != "https://push/ben-phone" && s.endpoint != "https://push/ben-laptop" {
			t.Errorf("pushed to %s", s.endpoint)
		}
		want := Payload{Title: "Weekend", Body: "Ada: dinner at 8?", URL: "/chat#" + roomID.String(), Tag: roomID.String()}
		if s.payload != want {
			t.Errorf("payload = %+v, want %+v", s.payload, want)
		}
		if len(s.topic) != 32 {
			t.Errorf("topic %q is not a 32-character id", s.topic)
		}
	}
}

func TestADirectMessageIsTitledWithTheAuthorAndDeadEndpointsAreDropped(t *testing.T) {
	n, store, sender, author, _, roomID := setup(t, domain.RoomKindDirect, fakePresence{})
	sender.status["https://push/ben-phone"] = http.StatusGone

	if err := n.Notify(t.Context(), domain.Message{ID: uuid.New(), RoomID: roomID, AuthorID: author, Kind: domain.MessageKindImage}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if got := sender.sent[0].payload; got.Title != "Ada" || got.Body != "Photo" {
		t.Errorf("payload = %+v, want Ada / Photo", got)
	}
	if len(store.dropped) != 1 || store.dropped[0] != "https://push/ben-phone" {
		t.Errorf("dropped %v, want the gone endpoint", store.dropped)
	}
}

func TestSystemLinesAreNotPushed(t *testing.T) {
	n, _, _, author, _, roomID := setup(t, domain.RoomKindGroup, fakePresence{})

	n.MessageSent(t.Context(), domain.Message{RoomID: roomID, AuthorID: author, Kind: domain.MessageKindSystem})
	if len(n.queue) != 0 {
		t.Fatal("a system line was queued for a push")
	}
	n.MessageSent(t.Context(), domain.Message{RoomID: roomID, AuthorID: author, Kind: domain.MessageKindText, Body: "hi"})
	if len(n.queue) != 1 {
		t.Fatal("a message was not queued for a push")
	}
}

func TestPreviews(t *testing.T) {
	long := make([]rune, 200)
	for i := range long {
		long[i] = 'я'
	}
	forwarded := uuid.New()
	tests := map[string]struct {
		m    domain.Message
		want string
	}{
		"text":                      {domain.Message{Kind: domain.MessageKindText, Body: " hello "}, "hello"},
		"captioned album":           {domain.Message{Kind: domain.MessageKindAlbum, Body: "trip"}, "Album: trip"},
		"voice":                     {domain.Message{Kind: domain.MessageKindVoice}, "Voice message"},
		"forwarded without words":   {domain.Message{Kind: domain.MessageKindImage, ForwardedFromID: &forwarded}, "Forwarded message"},
		"long text is cut by runes": {domain.Message{Kind: domain.MessageKindText, Body: string(long)}, string(long[:139]) + "…"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := previewOf(tt.m); got != tt.want {
				t.Errorf("previewOf = %q, want %q", got, tt.want)
			}
		})
	}
}

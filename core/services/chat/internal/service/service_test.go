package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

type harness struct {
	svc   *service.Service
	store *fakeStore
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	store := newFakeStore()
	svc, err := service.New(service.Config{Store: store, Logger: logger.Discard()})
	if err != nil {
		t.Fatalf("service.New: %v", err)
	}

	return &harness{svc: svc, store: store}
}

// openRoom creates a room and returns it with the id of whoever opened it.
func (h *harness) openRoom(t *testing.T, name string) (domain.Room, uuid.UUID) {
	t.Helper()

	owner := uuid.New()
	room, err := h.svc.CreateRoom(t.Context(), service.CreateRoomInput{
		Name:        name,
		UserID:      owner,
		DisplayName: "Owner",
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return room, owner
}

func TestCreateRoomRefusesAnEmptyName(t *testing.T) {
	h := newHarness(t)

	_, err := h.svc.CreateRoom(t.Context(), service.CreateRoomInput{
		Name:   "   ",
		UserID: uuid.New(),
	})

	v, ok := domain.AsValidationError(err)
	if !ok {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if v.Field != "name" {
		t.Errorf("the error names %q, want the field the caller can fix", v.Field)
	}
}

// The name is not the caller's own input — it comes from auth — so an unusable
// one costs the name, never the room.
func TestAnUnusableNameStillOpensTheRoom(t *testing.T) {
	h := newHarness(t)

	room, err := h.svc.CreateRoom(t.Context(), service.CreateRoomInput{
		Name:        "general",
		UserID:      uuid.New(),
		DisplayName: strings.Repeat("и", 500),
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	members, err := h.store.Members(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("the room holds %d members, want its creator", len(members))
	}
	if n := len([]rune(members[0].DisplayName)); n > domain.MaxRoomNameLength {
		t.Errorf("stored a %d-rune display name", n)
	}
}

// A room somebody is not in is indistinguishable from a room that does not
// exist. Both answers are ErrNotAMember, and the transport renders both the
// same way — anything else turns a room id into a probe.
func TestReadingARoomRequiresBelongingToIt(t *testing.T) {
	h := newHarness(t)

	room, _ := h.openRoom(t, "general")
	stranger := uuid.New()

	if _, err := h.svc.GetRoom(t.Context(), room.ID, stranger); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("GetRoom = %v, want ErrNotAMember", err)
	}
	if _, err := h.svc.ListMessages(t.Context(), room.ID, stranger, domain.Page{}); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("ListMessages = %v, want ErrNotAMember", err)
	}
	if _, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: stranger, Body: "hello",
	}); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("Send = %v, want ErrNotAMember", err)
	}

	// And a room that genuinely does not exist answers identically.
	if _, err := h.svc.GetRoom(t.Context(), uuid.New(), stranger); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("GetRoom on a missing room = %v, want ErrNotAMember", err)
	}
}

func TestJoiningIsIdempotentAndRefreshesTheName(t *testing.T) {
	h := newHarness(t)

	room, _ := h.openRoom(t, "general")
	joiner := uuid.New()

	_, joined, err := h.svc.JoinRoom(t.Context(), service.JoinRoomInput{
		RoomID: room.ID, UserID: joiner, DisplayName: "Ada",
	})
	if err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	if !joined {
		t.Error("a first join reported that nothing changed")
	}

	_, joined, err = h.svc.JoinRoom(t.Context(), service.JoinRoomInput{
		RoomID: room.ID, UserID: joiner, DisplayName: "Ada Lovelace",
	})
	if err != nil {
		t.Fatalf("second JoinRoom: %v", err)
	}
	if joined {
		t.Error("joining twice reported a second join")
	}

	members, err := h.store.Members(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	for _, m := range members {
		if m.UserID == joiner && m.DisplayName != "Ada Lovelace" {
			t.Errorf("display name = %q, want the name from the second join", m.DisplayName)
		}
	}
}

func TestJoiningARoomThatDoesNotExistIsRefused(t *testing.T) {
	h := newHarness(t)

	_, _, err := h.svc.JoinRoom(t.Context(), service.JoinRoomInput{
		RoomID: uuid.New(), UserID: uuid.New(),
	})
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

// The check that a socket cannot be trusted to have done once. A connection
// outlives the membership that justified opening it, and somebody removed from
// a room mid-conversation has to stop being able to write to it.
func TestSendingStopsTheMomentMembershipDoes(t *testing.T) {
	h := newHarness(t)

	room, owner := h.openRoom(t, "general")

	if _, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, Body: "while I am here",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if err := h.svc.LeaveRoom(t.Context(), room.ID, owner); err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}

	if _, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, Body: "and after",
	}); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("Send after leaving = %v, want ErrNotAMember", err)
	}
}

func TestSendValidatesWhatItIsGiven(t *testing.T) {
	h := newHarness(t)

	room, owner := h.openRoom(t, "general")

	for name, tc := range map[string]struct {
		in    service.SendInput
		field string
	}{
		"empty body": {
			in:    service.SendInput{RoomID: room.ID, AuthorID: owner, Body: "  "},
			field: "body",
		},
		"body over the limit": {
			in: service.SendInput{
				RoomID: room.ID, AuthorID: owner,
				Body: strings.Repeat("м", domain.MaxMessageLength+1),
			},
			field: "body",
		},
		"client id over the limit": {
			in: service.SendInput{
				RoomID: room.ID, AuthorID: owner, Body: "hello",
				ClientID: strings.Repeat("x", domain.MaxClientIDLength+1),
			},
			field: "client_id",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := h.svc.Send(t.Context(), tc.in)

			v, ok := domain.AsValidationError(err)
			if !ok {
				t.Fatalf("err = %v, want a validation error", err)
			}
			if v.Field != tc.field {
				t.Errorf("the error names %q, want %q", v.Field, tc.field)
			}
		})
	}
}

// The service passes the duplicate flag through rather than swallowing it: a
// resend has to be answered, and the caller above needs to know not to announce
// the message to the room a second time.
func TestAResendIsReportedAsOne(t *testing.T) {
	h := newHarness(t)

	room, owner := h.openRoom(t, "general")

	first, duplicate, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, Body: "hello", ClientID: "client-1",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if duplicate {
		t.Error("a first send was reported as a duplicate")
	}

	again, duplicate, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, Body: "hello", ClientID: "client-1",
	})
	if err != nil {
		t.Fatalf("resend: %v", err)
	}
	if !duplicate {
		t.Error("a resend was not reported as a duplicate")
	}
	if again.ID != first.ID {
		t.Errorf("resend produced %s, want the original %s", again.ID, first.ID)
	}
}

func TestListMessagesRefusesAPageBoundedBothWays(t *testing.T) {
	h := newHarness(t)

	room, owner := h.openRoom(t, "general")

	_, err := h.svc.ListMessages(t.Context(), room.ID, owner, domain.Page{
		BeforeSeq: 10,
		AfterSeq:  2,
	})
	if _, ok := domain.AsValidationError(err); !ok {
		t.Fatalf("err = %v, want a validation error", err)
	}
}

func TestListRoomsReturnsOnlyTheCallersOwn(t *testing.T) {
	h := newHarness(t)

	mine, me := h.openRoom(t, "mine")
	h.openRoom(t, "theirs")

	rooms, err := h.svc.ListRooms(t.Context(), me)
	if err != nil {
		t.Fatalf("ListRooms: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != mine.ID {
		t.Fatalf("listed %d rooms, want only the caller's own", len(rooms))
	}
}

// A store that is down must not look like a room that is missing: one is an
// outage and the other is the client's mistake, and a client that retries the
// wrong one wastes everybody's time.
func TestStoreFailuresDoNotBecomeDomainErrors(t *testing.T) {
	h := newHarness(t)

	room, owner := h.openRoom(t, "general")
	h.store.failWith = errors.New("connection refused")

	_, err := h.svc.GetRoom(t.Context(), room.ID, owner)
	if err == nil {
		t.Fatal("a failing store produced no error")
	}
	if errors.Is(err, domain.ErrNotAMember) || errors.Is(err, domain.ErrRoomNotFound) {
		t.Errorf("an outage was reported as %v", err)
	}
}

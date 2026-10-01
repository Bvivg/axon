//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func newRoom(t *testing.T, r *repository.Repository, name string) (domain.Room, uuid.UUID) {
	t.Helper()

	owner := uuid.New()
	room, err := r.CreateRoom(t.Context(), domain.Room{
		ID:        uuid.New(),
		Name:      name,
		CreatedBy: owner,
	}, domain.Member{UserID: owner, DisplayName: "Owner"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	return room, owner
}

func TestCreateRoomSeatsItsCreator(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

	if room.MemberCount != 1 {
		t.Errorf("member count = %d, want 1", room.MemberCount)
	}

	member, err := r.IsMember(t.Context(), room.ID, owner)
	if err != nil {
		t.Fatalf("IsMember: %v", err)
	}
	if !member {
		t.Error("the person who opened the room is not in it")
	}

	loaded, err := r.RoomByID(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("RoomByID: %v", err)
	}
	if loaded.Name != "general" || loaded.CreatedBy != owner || loaded.MemberCount != 1 {
		t.Errorf("RoomByID returned %+v, want the room that was just created", loaded)
	}
}

func TestRoomByIDReportsAMissingRoom(t *testing.T) {
	r := newRepo(t)

	if _, err := r.RoomByID(t.Context(), uuid.New()); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("err = %v, want ErrRoomNotFound", err)
	}
}

func TestRoomsForUserListsOnlyTheirOwn(t *testing.T) {
	r := newRepo(t)

	mine, me := newRoom(t, r, "mine")
	theirs, _ := newRoom(t, r, "theirs")

	rooms, err := r.RoomsForUser(t.Context(), me)
	if err != nil {
		t.Fatalf("RoomsForUser: %v", err)
	}
	if len(rooms) != 1 || rooms[0].ID != mine.ID {
		t.Fatalf("listed %d rooms, want only %s", len(rooms), mine.ID)
	}

	joined, err := r.AddMember(t.Context(), theirs.ID, domain.Member{UserID: me, DisplayName: "Me"})
	if err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if !joined {
		t.Error("a first join reported that nothing changed")
	}

	rooms, err = r.RoomsForUser(t.Context(), me)
	if err != nil {
		t.Fatalf("RoomsForUser: %v", err)
	}
	if len(rooms) != 2 {
		t.Errorf("listed %d rooms after joining, want 2", len(rooms))
	}
}

func TestJoiningTwiceIsNotAnErrorAndRefreshesTheName(t *testing.T) {
	r := newRepo(t)

	room, _ := newRoom(t, r, "general")
	joiner := uuid.New()

	joined, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: joiner, DisplayName: "Ada"})
	if err != nil {
		t.Fatalf("first AddMember: %v", err)
	}
	if !joined {
		t.Error("a first join reported that nothing changed")
	}

	joined, err = r.AddMember(t.Context(), room.ID, domain.Member{UserID: joiner, DisplayName: "Ada Lovelace"})
	if err != nil {
		t.Fatalf("second AddMember: %v", err)
	}
	if joined {
		t.Error("joining a room twice reported a second join")
	}

	members, err := r.Members(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("room holds %d members, want 2", len(members))
	}

	var found bool
	for _, m := range members {
		if m.UserID == joiner {
			found = true
			if m.DisplayName != "Ada Lovelace" {
				t.Errorf("display name = %q, want the name from the second join", m.DisplayName)
			}
		}
	}
	if !found {
		t.Error("the joiner is missing from the member list")
	}
}

func TestRoomsForUserCarriesHowFarTheOthersHaveRead(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")
	reader := uuid.New()
	if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: reader, DisplayName: "Reader"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	say(t, r, room.ID, owner, "one", "c-1")
	second := say(t, r, room.ID, owner, "two", "c-2")
	third := say(t, r, room.ID, owner, "three", "c-3")

	if err := r.MarkRead(t.Context(), room.ID, reader, second.Seq); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	for who, want := range map[uuid.UUID]int64{owner: second.Seq, reader: third.Seq} {
		rooms, err := r.RoomsForUser(t.Context(), who)
		if err != nil {
			t.Fatalf("RoomsForUser: %v", err)
		}
		if len(rooms) != 1 || rooms[0].OthersReadSeq != want {
			t.Errorf("others read up to %+v, want %d", rooms, want)
		}
	}
}

func TestLeavingKeepsTheHistory(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

	stayer := uuid.New()
	if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: stayer, DisplayName: "Stayer"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	if _, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID:       uuid.New(),
		RoomID:   room.ID,
		AuthorID: owner,
		Body:     "still here after I go",
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	if err := r.RemoveMember(t.Context(), room.ID, owner); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	member, err := r.IsMember(t.Context(), room.ID, owner)
	if err != nil {
		t.Fatalf("IsMember: %v", err)
	}
	if member {
		t.Error("still a member after leaving")
	}

	messages, _, err := r.ListMessages(t.Context(), room.ID, stayer, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("history holds %d messages after the author left, want 1", len(messages))
	}
}

func TestLeavingTwiceIsNotAnError(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

	for range 2 {
		if err := r.RemoveMember(t.Context(), room.ID, owner); err != nil {
			t.Fatalf("RemoveMember: %v", err)
		}
	}
}

func TestANewMessageBringsAHiddenChatBackWithoutItsOldHistory(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "hidden")

	peer := uuid.New()
	if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: peer, DisplayName: "Peer"}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	say(t, r, room.ID, owner, "before", "")

	if err := r.HideRoom(t.Context(), room.ID, peer); err != nil {
		t.Fatalf("HideRoom: %v", err)
	}
	rooms, err := r.RoomsForUser(t.Context(), peer)
	if err != nil || len(rooms) != 0 {
		t.Fatalf("rooms after hiding = %v, %v, want none", rooms, err)
	}

	after := say(t, r, room.ID, owner, "after", "")
	if len(after.Revealed) != 1 || after.Revealed[0] != peer {
		t.Errorf("revealed = %v, want the peer who hid the chat", after.Revealed)
	}

	rooms, err = r.RoomsForUser(t.Context(), peer)
	if err != nil || len(rooms) != 1 {
		t.Fatalf("rooms after a new message = %v, %v, want the chat back", rooms, err)
	}
	history, _, err := r.ListMessages(t.Context(), room.ID, peer, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(history) != 1 || history[0].Body != "after" {
		t.Errorf("history = %+v, want only the new message", history)
	}
}

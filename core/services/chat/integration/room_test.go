//go:build integration

package integration

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

// newRoom opens a room owned by a fresh user and returns both.
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

// A room with nobody in it is a state nothing else in the service knows how to
// handle: it cannot be listed, read or joined by anyone who does not already
// have its id. Creating one puts its creator in it, in the same transaction.
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

	// And it reads back the same way it was returned.
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

// Listing is by membership, not by existence: a room somebody else opened is
// not theirs to see until they are in it.
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

	// Joining the other one adds it, and reports that this call is what did it.
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

// A client that lost the answer and retried has done nothing wrong, so joining
// twice is not an error. Rejoining is also the only way a stale name snapshot
// gets corrected, so the second call refreshes it.
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

// Leaving takes somebody out of the room and leaves what they said in it: a
// message is not unsaid by its author walking out.
func TestLeavingKeepsTheHistory(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

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

	messages, _, err := r.ListMessages(t.Context(), room.ID, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("history holds %d messages after the author left, want 1", len(messages))
	}
}

// Leaving a room nobody is in should not be an error either: the caller wanted
// to not be a member, and they are not.
func TestLeavingTwiceIsNotAnError(t *testing.T) {
	r := newRepo(t)

	room, owner := newRoom(t, r, "general")

	for range 2 {
		if err := r.RemoveMember(t.Context(), room.ID, owner); err != nil {
			t.Fatalf("RemoveMember: %v", err)
		}
	}
}

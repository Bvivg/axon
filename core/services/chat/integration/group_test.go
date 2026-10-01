//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

func newGroup(t *testing.T, r *repository.Repository, members int) (domain.Room, uuid.UUID, []uuid.UUID) {
	t.Helper()

	owner := uuid.New()
	ids := make([]uuid.UUID, members)
	people := make([]domain.Member, members)
	for i := range people {
		ids[i] = uuid.New()
		people[i] = domain.Member{UserID: ids[i], DisplayName: "Member"}
	}

	room, err := r.CreateGroup(t.Context(),
		domain.Room{ID: uuid.New(), Name: "Weekend", CreatedBy: owner, Kind: domain.RoomKindGroup},
		domain.Member{UserID: owner, DisplayName: "Owner"},
		people,
	)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	return room, owner, ids
}

func TestAGroupSeatsItsOwnerAndMembers(t *testing.T) {
	r := newRepo(t)
	room, owner, members := newGroup(t, r, 2)

	loaded, err := r.RoomByID(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("RoomByID: %v", err)
	}
	if loaded.Kind != domain.RoomKindGroup || loaded.MemberCount != 3 {
		t.Errorf("group = %+v, want a three-person group", loaded)
	}

	roster, err := r.Members(t.Context(), room.ID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	roles := map[uuid.UUID]domain.MemberRole{}
	for _, m := range roster {
		roles[m.UserID] = m.Role
	}
	if roles[owner] != domain.MemberRoleOwner {
		t.Errorf("owner role = %q, want owner", roles[owner])
	}
	for _, id := range members {
		if roles[id] != domain.MemberRoleMember {
			t.Errorf("member role = %q, want member", roles[id])
		}
	}

	if _, err := r.MemberRole(t.Context(), room.ID, uuid.New()); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("stranger role err = %v, want ErrNotAMember", err)
	}
}

func TestTheEarliestMemberInheritsAnOwnerlessGroup(t *testing.T) {
	r := newRepo(t)
	room, owner, _ := newGroup(t, r, 0)

	first, second := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{first, second} {
		if _, err := r.AddMember(t.Context(), room.ID, domain.Member{UserID: id, Role: domain.MemberRoleMember}); err != nil {
			t.Fatalf("AddMember: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	if promoted, err := r.PromoteOwnerIfNone(t.Context(), room.ID); err != nil || promoted != nil {
		t.Fatalf("promote with an owner present = %v, %v, want nothing", promoted, err)
	}

	promoted, err := r.LeaveGroup(t.Context(), room.ID, owner)
	if err != nil {
		t.Fatalf("LeaveGroup: %v", err)
	}
	if member, _ := r.IsMember(t.Context(), room.ID, owner); member {
		t.Error("the owner is still in the group after leaving")
	}
	if promoted == nil || *promoted != first {
		t.Fatalf("promoted = %v, want the earliest member %s", promoted, first)
	}
	if role, _ := r.MemberRole(t.Context(), room.ID, first); role != domain.MemberRoleOwner {
		t.Errorf("new owner role = %q", role)
	}
	if role, _ := r.MemberRole(t.Context(), room.ID, second); role != domain.MemberRoleMember {
		t.Errorf("second member role = %q, want member", role)
	}
}

func TestRenamingAGroupIsReadBack(t *testing.T) {
	r := newRepo(t)
	room, _, _ := newGroup(t, r, 1)

	if err := r.RenameRoom(t.Context(), room.ID, "Trip"); err != nil {
		t.Fatalf("RenameRoom: %v", err)
	}
	loaded, err := r.RoomByID(t.Context(), room.ID)
	if err != nil || loaded.Name != "Trip" {
		t.Errorf("name = %q, %v, want Trip", loaded.Name, err)
	}
	if err := r.RenameRoom(t.Context(), uuid.New(), "Nowhere"); !errors.Is(err, domain.ErrRoomNotFound) {
		t.Errorf("rename missing = %v, want ErrRoomNotFound", err)
	}
}

func TestAddingStopsAtTheLimitAndCountsOnlyNewcomers(t *testing.T) {
	r := newRepo(t)
	room, _, members := newGroup(t, r, 1)

	newcomer := uuid.New()
	added, err := r.AddGroupMembers(t.Context(), room.ID, []domain.Member{
		{UserID: members[0], Role: domain.MemberRoleMember},
		{UserID: newcomer, Role: domain.MemberRoleMember},
	}, 3)
	if err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	if len(added) != 1 || added[0] != newcomer {
		t.Errorf("added = %v, want only the newcomer", added)
	}

	late := uuid.New()
	_, err = r.AddGroupMembers(t.Context(), room.ID, []domain.Member{{UserID: late, Role: domain.MemberRoleMember}}, 3)
	if !errors.Is(err, domain.ErrGroupFull) {
		t.Fatalf("adding past the limit = %v, want ErrGroupFull", err)
	}
	if seated, err := r.IsMember(t.Context(), room.ID, late); err != nil || seated {
		t.Errorf("refused person seated = %v, %v, want not seated", seated, err)
	}
}

func TestAddingCountsWhoJoinedWhileItWaitedForTheGroup(t *testing.T) {
	r := newRepo(t)
	room, _, _ := newGroup(t, r, 1)

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(t.Context(), `SELECT id FROM rooms WHERE id = $1 FOR NO KEY UPDATE`, room.ID); err != nil {
		t.Fatalf("lock the group: %v", err)
	}
	if _, err := tx.Exec(t.Context(),
		`INSERT INTO room_members (room_id, user_id) VALUES ($1, $2)`, room.ID, uuid.New()); err != nil {
		t.Fatalf("seat someone: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := r.AddGroupMembers(context.Background(), room.ID,
			[]domain.Member{{UserID: uuid.New(), Role: domain.MemberRoleMember}}, 3)
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(t.Context(),
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`,
		).Scan(&waiting); err != nil {
			t.Fatalf("look for the waiting add: %v", err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the add never waited for the group")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; !errors.Is(err, domain.ErrGroupFull) {
		t.Errorf("adding a fourth person to a group of three = %v, want ErrGroupFull", err)
	}
}

func TestSomeoneAddedBackDoesNotReadWhatWasSaidWhileAway(t *testing.T) {
	r := newRepo(t)
	room, owner, members := newGroup(t, r, 1)
	carol := members[0]

	say(t, r, room.ID, owner, "before", "")
	if err := r.RemoveMember(t.Context(), room.ID, carol); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	away := say(t, r, room.ID, owner, "while away", "")

	if _, err := r.AddGroupMembers(t.Context(), room.ID,
		[]domain.Member{{UserID: carol, Role: domain.MemberRoleMember}}, domain.MaxGroupMembers); err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	if start, err := r.HistoryStart(t.Context(), room.ID, carol); err != nil || start != away.Seq {
		t.Errorf("history start = %d, %v, want %d", start, err, away.Seq)
	}
	say(t, r, room.ID, owner, "after", "")

	history, _, err := r.ListMessages(t.Context(), room.ID, carol, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(history) != 1 || history[0].Body != "after" {
		t.Errorf("history = %+v, want only what was said after coming back", history)
	}
}

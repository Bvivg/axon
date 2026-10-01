package service_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

func (h *harness) group(t *testing.T, members int) (domain.Room, uuid.UUID, []uuid.UUID) {
	t.Helper()

	owner := uuid.New()
	people := make([]service.GroupMember, members)
	ids := make([]uuid.UUID, members)
	for i := range people {
		ids[i] = uuid.New()
		people[i] = service.GroupMember{UserID: ids[i], DisplayName: "Member"}
	}

	change, err := h.svc.CreateGroup(t.Context(), service.CreateGroupInput{
		Name:    "Weekend",
		Owner:   service.GroupMember{UserID: owner, DisplayName: "Owner"},
		Members: people,
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	return change.Room, owner, ids
}

func systemEvent(t *testing.T, m domain.Message) string {
	t.Helper()

	if m.Kind != domain.MessageKindSystem {
		t.Fatalf("kind = %q, want system", m.Kind)
	}
	var payload domain.SystemPayload
	if err := json.Unmarshal(m.Payload, &payload); err != nil {
		t.Fatalf("decode system payload: %v", err)
	}
	return payload.Event
}

func TestCreateGroupMakesTheCreatorOwnerAndAnnouncesEveryone(t *testing.T) {
	h := newHarness(t)

	owner, first, second := uuid.New(), uuid.New(), uuid.New()
	change, err := h.svc.CreateGroup(t.Context(), service.CreateGroupInput{
		Name:  "  Weekend  ",
		Owner: service.GroupMember{UserID: owner, DisplayName: "Owner"},
		Members: []service.GroupMember{
			{UserID: first, DisplayName: "First"},
			{UserID: second, DisplayName: "Second"},
		},
	})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	if change.Room.Kind != domain.RoomKindGroup || change.Room.Name != "Weekend" || change.Room.MemberCount != 3 {
		t.Errorf("room = %+v, want a three-person group named Weekend", change.Room)
	}
	if len(change.Added) != 3 {
		t.Errorf("added = %v, want the owner and both members", change.Added)
	}
	if len(change.Events) != 1 || systemEvent(t, change.Events[0]) != domain.SystemGroupCreated ||
		change.Events[0].Body != "Weekend" {
		t.Errorf("events = %+v, want one group_created carrying the name", change.Events)
	}

	role, err := h.store.MemberRole(t.Context(), change.Room.ID, owner)
	if err != nil || role != domain.MemberRoleOwner {
		t.Errorf("creator role = %q, %v, want owner", role, err)
	}
	role, err = h.store.MemberRole(t.Context(), change.Room.ID, first)
	if err != nil || role != domain.MemberRoleMember {
		t.Errorf("member role = %q, %v, want member", role, err)
	}
}

func TestCreateGroupNeedsANameAndSomeoneElse(t *testing.T) {
	h := newHarness(t)
	owner := service.GroupMember{UserID: uuid.New()}

	for name, in := range map[string]service.CreateGroupInput{
		"no name":   {Name: " ", Owner: owner, Members: []service.GroupMember{{UserID: uuid.New()}}},
		"no people": {Name: "Alone", Owner: owner},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.CreateGroup(t.Context(), in)
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("err = %v, want a validation error", err)
			}
		})
	}
}

func TestGroupsCannotBeJoinedByID(t *testing.T) {
	h := newHarness(t)
	room, _, _ := h.group(t, 1)

	_, _, err := h.svc.JoinRoom(t.Context(), service.JoinRoomInput{RoomID: room.ID, UserID: uuid.New()})
	if !errors.Is(err, domain.ErrGroupNotJoinable) {
		t.Fatalf("err = %v, want ErrGroupNotJoinable", err)
	}
}

func TestOnlyTheOwnerManagesTheGroup(t *testing.T) {
	h := newHarness(t)
	room, _, members := h.group(t, 2)
	member := members[0]

	if _, err := h.svc.AddGroupMembers(t.Context(), room.ID, member,
		[]service.GroupMember{{UserID: uuid.New()}}); !errors.Is(err, domain.ErrNotGroupOwner) {
		t.Errorf("add by member = %v, want ErrNotGroupOwner", err)
	}
	if _, err := h.svc.RemoveGroupMember(t.Context(), room.ID, member, members[1]); !errors.Is(err, domain.ErrNotGroupOwner) {
		t.Errorf("remove by member = %v, want ErrNotGroupOwner", err)
	}
	if _, err := h.svc.RenameGroup(t.Context(), room.ID, member, "Mine now"); !errors.Is(err, domain.ErrNotGroupOwner) {
		t.Errorf("rename by member = %v, want ErrNotGroupOwner", err)
	}
	if _, err := h.svc.RenameGroup(t.Context(), room.ID, uuid.New(), "Outsider"); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("rename by outsider = %v, want ErrNotAMember", err)
	}
}

func TestOwnerAddsOnlyNewPeople(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 1)
	newcomer := uuid.New()

	change, err := h.svc.AddGroupMembers(t.Context(), room.ID, owner, []service.GroupMember{
		{UserID: members[0], DisplayName: "Already here"},
		{UserID: newcomer, DisplayName: "Newcomer"},
	})
	if err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}

	if len(change.Added) != 1 || change.Added[0] != newcomer {
		t.Errorf("added = %v, want only the newcomer", change.Added)
	}
	if len(change.Events) != 1 || systemEvent(t, change.Events[0]) != domain.SystemMemberAdded {
		t.Errorf("events = %+v, want one member_added", change.Events)
	}
	if change.Room.MemberCount != 3 {
		t.Errorf("member count = %d, want 3", change.Room.MemberCount)
	}
}

func TestOwnerRemovesAMemberButNotThemselves(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 2)

	if _, err := h.svc.RemoveGroupMember(t.Context(), room.ID, owner, owner); err == nil {
		t.Error("the owner removed themselves, want a validation error")
	}
	if _, err := h.svc.RemoveGroupMember(t.Context(), room.ID, owner, uuid.New()); err == nil {
		t.Error("removing a stranger succeeded, want a validation error")
	}

	change, err := h.svc.RemoveGroupMember(t.Context(), room.ID, owner, members[0])
	if err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if len(change.Removed) != 1 || change.Removed[0] != members[0] {
		t.Errorf("removed = %v, want %s", change.Removed, members[0])
	}
	if systemEvent(t, change.Events[0]) != domain.SystemMemberRemoved {
		t.Errorf("event = %+v, want member_removed", change.Events[0])
	}

	if _, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: members[0], Body: "still here?",
	}); !errors.Is(err, domain.ErrNotAMember) {
		t.Errorf("send by removed member = %v, want ErrNotAMember", err)
	}
}

func TestRenameAnnouncesTheNewNameOnce(t *testing.T) {
	h := newHarness(t)
	room, owner, _ := h.group(t, 1)

	change, err := h.svc.RenameGroup(t.Context(), room.ID, owner, "Trip")
	if err != nil {
		t.Fatalf("RenameGroup: %v", err)
	}
	if change.Room.Name != "Trip" || len(change.Events) != 1 || change.Events[0].Body != "Trip" {
		t.Errorf("change = %+v, want the new name announced", change)
	}

	again, err := h.svc.RenameGroup(t.Context(), room.ID, owner, "Trip")
	if err != nil {
		t.Fatalf("RenameGroup again: %v", err)
	}
	if len(again.Events) != 0 {
		t.Errorf("renaming to the same name announced %d events, want none", len(again.Events))
	}
}

func TestWhenTheOwnerLeavesTheEarliestMemberTakesOver(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 2)

	change, err := h.svc.LeaveRoom(t.Context(), room.ID, owner)
	if err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if len(change.Removed) != 1 || change.Removed[0] != owner {
		t.Errorf("removed = %v, want the owner", change.Removed)
	}
	if len(change.Events) != 1 || systemEvent(t, change.Events[0]) != domain.SystemMemberLeft {
		t.Errorf("events = %+v, want member_left", change.Events)
	}

	owners := 0
	for _, id := range members {
		if role, _ := h.store.MemberRole(t.Context(), room.ID, id); role == domain.MemberRoleOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Errorf("owners after the owner left = %d, want exactly 1", owners)
	}
}

func TestTheGroupKeepsAnOwnerWhenTheLeaveCannotBeAnnounced(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 2)

	h.store.failAppend = errors.New("database hiccup")
	change, err := h.svc.LeaveRoom(t.Context(), room.ID, owner)
	h.store.failAppend = nil
	if err != nil {
		t.Fatalf("LeaveRoom: %v", err)
	}
	if !slices.Equal(change.Removed, []uuid.UUID{owner}) || len(change.Events) != 0 {
		t.Errorf("change = %+v, want the owner removed and nothing announced", change)
	}

	if member, _ := h.store.IsMember(t.Context(), room.ID, owner); member {
		t.Fatal("the owner is still in the group")
	}
	owners := 0
	for _, id := range members {
		if role, _ := h.store.MemberRole(t.Context(), room.ID, id); role == domain.MemberRoleOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Errorf("owners after the owner left = %d, want exactly 1", owners)
	}
}

func TestARemovalThatCannotBeAnnouncedStillReachesTheRemovedPerson(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 2)

	h.store.failAppend = errors.New("database hiccup")
	change, err := h.svc.RemoveGroupMember(t.Context(), room.ID, owner, members[0])
	h.store.failAppend = nil
	if err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if !slices.Equal(change.Removed, []uuid.UUID{members[0]}) {
		t.Errorf("removed = %v, want %s so they are told", change.Removed, members[0])
	}
}

func TestDirectChatsCannotBeLeft(t *testing.T) {
	h := newHarness(t)
	alice, bob := uuid.New(), uuid.New()

	_, room, _, err := h.svc.SendDirect(t.Context(), service.SendDirectInput{
		FromUserID: alice, ToUserID: bob, Body: "hi",
	})
	if err != nil {
		t.Fatalf("SendDirect: %v", err)
	}

	if _, err := h.svc.LeaveRoom(t.Context(), room.ID, alice); !errors.Is(err, domain.ErrNotAGroup) {
		t.Fatalf("err = %v, want ErrNotAGroup", err)
	}
}

func TestAddingPastTheLimitIsRefusedButCurrentMembersDoNotCount(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, domain.MaxGroupMembers-2)

	newcomer := uuid.New()
	change, err := h.svc.AddGroupMembers(t.Context(), room.ID, owner, []service.GroupMember{
		{UserID: members[0], DisplayName: "Member"},
		{UserID: newcomer, DisplayName: "Newcomer"},
	})
	if err != nil {
		t.Fatalf("AddGroupMembers with one newcomer: %v", err)
	}
	if len(change.Added) != 1 || change.Added[0] != newcomer {
		t.Errorf("added = %v, want only the newcomer", change.Added)
	}

	_, err = h.svc.AddGroupMembers(t.Context(), room.ID, owner, []service.GroupMember{{UserID: uuid.New()}})
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		t.Errorf("adding to a full group = %v, want a validation error", err)
	}
}

func TestLeavingARoomYouAreNotInIsQuiet(t *testing.T) {
	h := newHarness(t)
	room, _, _ := h.group(t, 1)

	change, err := h.svc.LeaveRoom(t.Context(), room.ID, uuid.New())
	if err != nil {
		t.Fatalf("LeaveRoom by a stranger: %v", err)
	}
	if len(change.Removed) != 0 || len(change.Events) != 0 {
		t.Errorf("change = %+v, want nothing to announce", change)
	}
}

func TestAddingSeveralPeopleIsOneAnnouncement(t *testing.T) {
	h := newHarness(t)
	room, owner, _ := h.group(t, 1)

	first, second := uuid.New(), uuid.New()
	change, err := h.svc.AddGroupMembers(t.Context(), room.ID, owner, []service.GroupMember{
		{UserID: first, DisplayName: "First"},
		{UserID: second, DisplayName: "Second"},
	})
	if err != nil {
		t.Fatalf("AddGroupMembers: %v", err)
	}
	if len(change.Events) != 1 {
		t.Fatalf("events = %d, want one announcement for both", len(change.Events))
	}

	var payload domain.SystemPayload
	if err := json.Unmarshal(change.Events[0].Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	want := []string{first.String(), second.String()}
	if payload.Event != domain.SystemMemberAdded || !slices.Equal(payload.TargetIDs, want) {
		t.Errorf("payload = %+v, want member_added naming %v", payload, want)
	}
}

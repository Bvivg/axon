//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
)

func (u *user) createGroup(t *testing.T, name string, members ...*user) *chatv1.Room {
	t.Helper()

	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.id
	}
	created, err := u.chat.CreateGroup(context.Background(), connect.NewRequest(&chatv1.CreateGroupRequest{
		Name: name, MemberUserIds: ids,
	}))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	return created.Msg.GetRoom()
}

func (u *user) roles(t *testing.T, roomID string) map[string]chatv1.MemberRole {
	t.Helper()

	got, err := u.chat.GetRoom(context.Background(), connect.NewRequest(&chatv1.GetRoomRequest{RoomId: roomID}))
	if err != nil {
		t.Fatalf("get room: %v", err)
	}
	roles := map[string]chatv1.MemberRole{}
	for _, m := range got.Msg.GetMembers() {
		roles[m.GetUserId()] = m.GetRole()
	}
	return roles
}

func systemEvent(t *testing.T, got outbound) string {
	t.Helper()

	if got.Message == nil || got.Message.Kind != "system" {
		t.Fatalf("got %+v, want a system message", got)
	}
	var payload struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(got.Message.Payload, &payload); err != nil {
		t.Fatalf("decode system payload: %v", err)
	}
	return payload.Event
}

func wantCode(t *testing.T, what string, err error, code connect.Code) {
	t.Helper()

	var ce *connect.Error
	if !errors.As(err, &ce) || ce.Code() != code {
		t.Errorf("%s = %v, want %s", what, err, code)
	}
}

func TestAGroupIsBuiltAndRunByItsOwner(t *testing.T) {
	alice, bob, carol, dave := newUser(t), newUser(t), newUser(t), newUser(t)
	bs, cs, ds := bob.connect(t), carol.connect(t), dave.connect(t)

	group := alice.createGroup(t, "e2e group", bob, carol)
	if group.GetKind() != chatv1.RoomKind_ROOM_KIND_GROUP || group.GetMemberCount() != 3 {
		t.Fatalf("group = %+v, want a three-person group", group)
	}
	for name, s := range map[string]*socket{"bob": bs, "carol": cs} {
		if got := s.expect(typeRoomAdded); got.RoomID != group.GetId() {
			t.Errorf("%s was told about %q, want %q", name, got.RoomID, group.GetId())
		}
	}

	_, err := dave.chat.JoinRoom(context.Background(), connect.NewRequest(&chatv1.JoinRoomRequest{RoomId: group.GetId()}))
	wantCode(t, "joining a group by id", err, connect.CodeFailedPrecondition)

	_, err = alice.chat.CreateGroup(context.Background(), connect.NewRequest(&chatv1.CreateGroupRequest{
		Name: "ghosts", MemberUserIds: []string{uuid.NewString()},
	}))
	wantCode(t, "a group with a stranger's id", err, connect.CodeInvalidArgument)

	bs.subscribe(group.GetId(), 0)

	if _, err := alice.chat.AddGroupMembers(context.Background(), connect.NewRequest(&chatv1.AddGroupMembersRequest{
		RoomId: group.GetId(), UserIds: []string{dave.id},
	})); err != nil {
		t.Fatalf("add dave: %v", err)
	}
	if got := ds.expect(typeRoomAdded); got.RoomID != group.GetId() {
		t.Errorf("dave was told about %q", got.RoomID)
	}
	if event := systemEvent(t, bs.expect(typeMessage)); event != "member_added" {
		t.Errorf("bob saw %q, want member_added", event)
	}

	_, err = bob.chat.RenameGroup(context.Background(), connect.NewRequest(&chatv1.RenameGroupRequest{
		RoomId: group.GetId(), Name: "bob's now",
	}))
	wantCode(t, "a member renaming", err, connect.CodePermissionDenied)

	if _, err := alice.chat.RemoveGroupMember(context.Background(), connect.NewRequest(&chatv1.RemoveGroupMemberRequest{
		RoomId: group.GetId(), UserId: carol.id,
	})); err != nil {
		t.Fatalf("remove carol: %v", err)
	}
	if got := cs.expect(typeRoomRemoved); got.RoomID != group.GetId() {
		t.Errorf("carol was told about %q", got.RoomID)
	}
	if event := systemEvent(t, bs.expect(typeMessage)); event != "member_removed" {
		t.Errorf("bob saw %q, want member_removed", event)
	}
	_, err = carol.chat.ListMessages(context.Background(), connect.NewRequest(&chatv1.ListMessagesRequest{RoomId: group.GetId()}))
	if err == nil {
		t.Error("carol still reads the group after being removed")
	}

	if _, err := alice.chat.LeaveRoom(context.Background(), connect.NewRequest(&chatv1.LeaveRoomRequest{
		RoomId: group.GetId(),
	})); err != nil {
		t.Fatalf("alice leaves: %v", err)
	}
	if event := systemEvent(t, bs.expect(typeMessage)); event != "member_left" {
		t.Errorf("bob saw %q, want member_left", event)
	}

	roles := bob.roles(t, group.GetId())
	if roles[bob.id] != chatv1.MemberRole_MEMBER_ROLE_OWNER || roles[dave.id] != chatv1.MemberRole_MEMBER_ROLE_MEMBER {
		t.Errorf("roles after the owner left = %v, want bob to own the group", roles)
	}

	renamed, err := bob.chat.RenameGroup(context.Background(), connect.NewRequest(&chatv1.RenameGroupRequest{
		RoomId: group.GetId(), Name: "bob's now",
	}))
	if err != nil {
		t.Fatalf("the new owner renames: %v", err)
	}
	if renamed.Msg.GetRoom().GetName() != "bob's now" {
		t.Errorf("name = %q", renamed.Msg.GetRoom().GetName())
	}
	renameEvent := bs.expect(typeMessage)
	if systemEvent(t, renameEvent) != "group_renamed" || renameEvent.Message.Body != "bob's now" {
		t.Errorf("bob saw %+v, want group_renamed carrying the new name", renameEvent.Message)
	}
}

func TestEditsAndDeletesReachThePeerLive(t *testing.T) {
	alice, bob := newUser(t), newUser(t)
	as, bs := alice.connect(t), bob.connect(t)

	as.send(inbound{Type: typeSend, ToUserID: bob.id, ClientID: "edit-1", Body: "hi"})
	ack := as.expect(typeAck)
	bs.expect(typeRoomAdded)
	as.expect(typeRoomAdded)

	bs.subscribe(ack.RoomID, 0)
	as.send(inbound{Type: typeSend, RoomID: ack.RoomID, ClientID: "edit-2", Body: "helo"})
	as.expect(typeAck)
	sent := bs.expect(typeMessage)

	as.send(inbound{Type: typeEdit, MessageID: sent.Message.ID, Body: "hello"})
	edited := bs.expect(typeMessageUpdated)
	if edited.Message.ID != sent.Message.ID || edited.Message.Body != "hello" || edited.Message.EditedAt == nil {
		t.Errorf("bob saw %+v, want the message edited to hello", edited.Message)
	}

	bs.send(inbound{Type: typeDelete, MessageID: sent.Message.ID})
	if refusal := bs.expect(typeError); refusal.Code != errorForbidden {
		t.Errorf("bob deleting alice's message was refused with %q, want %q", refusal.Code, errorForbidden)
	}

	as.send(inbound{Type: typeDelete, MessageID: sent.Message.ID})
	deleted := bs.expect(typeMessageUpdated)
	if deleted.Message.DeletedAt == nil || deleted.Message.Body != "" {
		t.Errorf("bob saw %+v, want the message deleted", deleted.Message)
	}

	history, err := bob.chat.ListMessages(context.Background(), connect.NewRequest(&chatv1.ListMessagesRequest{
		RoomId: ack.RoomID,
	}))
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(history.Msg.GetMessages()) != 2 {
		t.Fatalf("history holds %d messages, want 2", len(history.Msg.GetMessages()))
	}
	stored := history.Msg.GetMessages()[1]
	if stored.GetDeletedAt() == nil || stored.GetBody() != "" || stored.GetEditedAt() == nil {
		t.Errorf("stored = %+v, want an edited then deleted placeholder", stored)
	}
}

func TestAReplyAndAForwardCarryTheirContext(t *testing.T) {
	alice, bob := newUser(t), newUser(t)
	as, bs := alice.connect(t), bob.connect(t)

	as.send(inbound{Type: typeSend, ToUserID: bob.id, ClientID: "ctx-1", Body: "hi"})
	ack := as.expect(typeAck)
	bs.expect(typeRoomAdded)
	as.expect(typeRoomAdded)

	as.subscribe(ack.RoomID, 0)
	as.send(inbound{Type: typeSend, RoomID: ack.RoomID, ClientID: "ctx-2", Body: "the original words"})
	as.expect(typeAck)
	original := as.expect(typeMessage)

	bs.send(inbound{Type: typeSend, RoomID: ack.RoomID, ClientID: "ctx-3", Body: "agreed", ReplyToID: original.Message.ID})
	bs.expect(typeAck)
	reply := as.expect(typeMessage)
	if reply.Message.ReplyTo == nil || reply.Message.ReplyTo.AuthorID != alice.id ||
		reply.Message.ReplyTo.Body != "the original words" {
		t.Errorf("reply = %+v, want a preview of alice's words", reply.Message.ReplyTo)
	}

	group := bob.createGroup(t, "forwards", alice)
	as.expect(typeRoomAdded)
	bs.expect(typeRoomAdded)
	as.subscribe(group.GetId(), 0)

	bs.send(inbound{
		Type: typeSend, RoomID: group.GetId(), ClientID: "ctx-4", Body: "forged", ForwardedFromID: original.Message.ID,
	})
	bs.expect(typeAck)
	forwarded := as.expect(typeMessage)
	if forwarded.Message.ForwardedFromAuthorID != alice.id || forwarded.Message.Body != "the original words" {
		t.Errorf("forward = %+v, want alice's original words credited to her", forwarded.Message)
	}
}

//go:build e2e

package e2e

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func TestAFirstDirectMessageAnnouncesTheRoomToThePeer(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	as, bs := alice.connect(t), bob.connect(t)

	as.send(inbound{Type: typeSend, ToUserID: bob.id, ClientID: "alice-dm-1", Body: "hi bob"})
	ack := as.expect(typeAck)

	added := bs.expect(typeRoomAdded)
	if added.RoomID != ack.RoomID {
		t.Fatalf("bob was told about room %q, want %q", added.RoomID, ack.RoomID)
	}

	rooms, err := bob.chat.ListRooms(context.Background(), connect.NewRequest(&chatv1.ListRoomsRequest{}))
	if err != nil {
		t.Fatalf("list bob's rooms: %v", err)
	}
	for _, room := range rooms.Msg.GetRooms() {
		if room.GetId() == ack.RoomID {
			if room.GetUnreadCount() != 1 {
				t.Errorf("unread = %d, want 1", room.GetUnreadCount())
			}
			return
		}
	}
	t.Errorf("the announced room %s is missing from bob's list", ack.RoomID)
}

func TestTypingAndReadingReachThePeerLive(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	room := alice.createRoom(t, "e2e typing")
	bob.joinRoom(t, room.GetId())

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)

	bs.send(inbound{Type: typeTyping, RoomID: room.GetId()})
	if typing := as.expect(typeTyping); typing.UserID != bob.id || typing.RoomID != room.GetId() {
		t.Errorf("alice saw %s typing in %s, want bob in %s", typing.UserID, typing.RoomID, room.GetId())
	}

	as.send(inbound{Type: typeSend, RoomID: room.GetId(), ClientID: "alice-1", Body: "read me"})
	ack := as.expect(typeAck)
	as.expect(typeMessage)
	bs.expect(typeMessage)

	if _, err := bob.chat.MarkRead(context.Background(), connect.NewRequest(&chatv1.MarkReadRequest{
		RoomId: room.GetId(),
		Seq:    ack.Seq,
	})); err != nil {
		t.Fatalf("bob marks the room read: %v", err)
	}

	read := as.expect(typeRead)
	if read.UserID != bob.id || read.Seq != ack.Seq {
		t.Errorf("alice got a read receipt from %s up to %d, want bob up to %d", read.UserID, read.Seq, ack.Seq)
	}
}

func TestPresenceIsPushedAndLastSeenIsRecorded(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	probe := alice.createRoom(t, "e2e presence probe")

	as := alice.connect(t)
	as.send(inbound{Type: typeWatchPresence, ToUserID: bob.id})
	as.subscribe(probe.GetId(), 0)

	if profile := alice.profileOf(t, bob.id); profile.GetOnline() {
		t.Fatal("bob is online before opening any socket")
	}

	presence := bob.goOnline(t)

	if pushed := as.expect(typePresence); pushed.UserID != bob.id {
		t.Fatalf("presence pushed for %s, want bob", pushed.UserID)
	}
	if profile := alice.profileOf(t, bob.id); !profile.GetOnline() {
		t.Fatal("bob is offline while his presence socket is open")
	}

	if err := presence.Close(ws.StatusNormalClosure, "bye"); err != nil {
		t.Fatalf("close the presence socket: %v", err)
	}

	if pushed := as.expect(typePresence); pushed.UserID != bob.id {
		t.Fatalf("presence pushed for %s, want bob", pushed.UserID)
	}

	profile := alice.profileOf(t, bob.id)
	if profile.GetOnline() {
		t.Error("bob is still online after closing his presence socket")
	}
	if profile.GetLastSeenAt() == nil {
		t.Error("bob's last seen time was not recorded")
	}
}

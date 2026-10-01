//go:build e2e

package e2e

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
)

func TestForwardingABatchReachesEveryRoomInOrderAndShowsWhoRead(t *testing.T) {
	alice, bob, carol := newUser(t), newUser(t), newUser(t)
	source := alice.createRoom(t, "e2e forward source")
	toBob := alice.createRoom(t, "e2e forward to bob")
	toCarol := alice.createRoom(t, "e2e forward to carol")
	bob.joinRoom(t, source.GetId())
	bob.joinRoom(t, toBob.GetId())
	carol.joinRoom(t, toCarol.GetId())

	as := alice.connect(t)
	as.subscribe(source.GetId(), 0)
	var ids []string
	for i, body := range []string{"first", "second"} {
		as.send(inbound{Type: typeSend, RoomID: source.GetId(), ClientID: "fwd-src-" + string(rune('a'+i)), Body: body})
		as.expect(typeAck)
		ids = append(ids, as.expect(typeMessage).Message.ID)
	}

	bs, cs := bob.connect(t), carol.connect(t)
	bs.subscribe(toBob.GetId(), 0)
	cs.subscribe(toCarol.GetId(), 0)

	resp, err := alice.chat.ForwardMessages(context.Background(), connect.NewRequest(&chatv1.ForwardMessagesRequest{
		MessageIds: ids,
		RoomIds:    []string{toBob.GetId(), toCarol.GetId()},
		Comment:    "have a look",
		ClientId:   "e2e-batch-1",
	}))
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if resp.Msg.GetSent() != 6 {
		t.Fatalf("sent = %d, want a comment and two copies in each of two rooms", resp.Msg.GetSent())
	}

	for name, s := range map[string]*socket{"bob": bs, "carol": cs} {
		comment := s.expect(typeMessage).Message
		if comment.Body != "have a look" || comment.ForwardedFromID != "" {
			t.Errorf("%s first got %+v, want the comment", name, comment)
		}
		for i, want := range []string{"first", "second"} {
			copied := s.expect(typeMessage).Message
			if copied.ForwardedFromID != ids[i] || copied.Body != want || copied.ForwardedFromAuthorID != alice.id {
				t.Errorf("%s copy %d = %+v, want a forward of %q by alice", name, i, copied, want)
			}
		}
	}

	if _, err := bob.chat.MarkRead(context.Background(), connect.NewRequest(&chatv1.MarkReadRequest{
		RoomId: source.GetId(),
		Seq:    1 << 40,
	})); err != nil {
		t.Fatalf("bob marks the source read: %v", err)
	}
	readers, err := alice.chat.ListMessageReaders(context.Background(), connect.NewRequest(&chatv1.ListMessageReadersRequest{
		MessageId: ids[1],
	}))
	if err != nil {
		t.Fatalf("list readers: %v", err)
	}
	got := readers.Msg.GetReaders()
	if len(got) != 1 || got[0].GetUserId() != bob.id || got[0].GetReadAt() == nil {
		t.Fatalf("readers = %+v, want bob with a time", got)
	}

	_, err = bob.chat.ListMessageReaders(context.Background(), connect.NewRequest(&chatv1.ListMessageReadersRequest{
		MessageId: ids[1],
	}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("bob asking about alice's message got %v, want permission denied", err)
	}
}

func TestDeletingSeveralOwnMessagesAtOnce(t *testing.T) {
	alice, bob := newUser(t), newUser(t)
	room := alice.createRoom(t, "e2e bulk delete")
	bob.joinRoom(t, room.GetId())

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)

	var ids []string
	for i, body := range []string{"one", "two", "three"} {
		as.send(inbound{Type: typeSend, RoomID: room.GetId(), ClientID: "bulk-" + string(rune('a'+i)), Body: body})
		as.expect(typeAck)
		ids = append(ids, as.expect(typeMessage).Message.ID)
		bs.expect(typeMessage)
	}

	as.send(inbound{Type: typeDelete, MessageIDs: ids[:2]})
	for i := range 2 {
		updated := bs.expect(typeMessageUpdated).Message
		if updated.ID != ids[i] || updated.DeletedAt == nil {
			t.Errorf("update %d = %+v, want %s deleted", i, updated, ids[i])
		}
	}
}

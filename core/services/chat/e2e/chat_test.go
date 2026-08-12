//go:build e2e

package e2e

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func TestAMessageReachesTheOtherPerson(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	room := alice.createRoom(t, "e2e general")
	bob.joinRoom(t, room.GetId())

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)

	as.send(inbound{
		Type: typeSend, RoomID: room.GetId(), ClientID: "alice-1", Body: "hello from the other side",
	})

	ack := as.expect(typeAck)
	if ack.ClientID != "alice-1" {
		t.Errorf("ack carries %q, want alice-1", ack.ClientID)
	}
	if ack.Seq != 1 {
		t.Errorf("the first message landed at position %d, want 1", ack.Seq)
	}

	got := bs.expect(typeMessage)
	if got.Message.Body != "hello from the other side" {
		t.Errorf("bob received %q", got.Message.Body)
	}
	if got.Message.AuthorID != alice.id {
		t.Errorf("attributed to %s, want %s", got.Message.AuthorID, alice.id)
	}

	if got.Message.ClientID != "" {
		t.Errorf("bob received alice's client id %q", got.Message.ClientID)
	}
}

func TestWhatWasSaidOnTheSocketIsInTheHistory(t *testing.T) {
	alice := newUser(t)

	room := alice.createRoom(t, "e2e history")
	as := alice.connect(t)
	as.subscribe(room.GetId(), 0)

	said := []string{"first", "second", "third"}
	for i, body := range said {
		as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: body})
		if ack := as.expect(typeAck); ack.Seq != int64(i+1) {
			t.Fatalf("%q landed at %d, want %d", body, ack.Seq, i+1)
		}
		as.expect(typeMessage)
	}

	history, err := alice.chat.ListMessages(context.Background(),
		connect.NewRequest(&chatv1.ListMessagesRequest{RoomId: room.GetId()}))
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}

	messages := history.Msg.GetMessages()
	if len(messages) != len(said) {
		t.Fatalf("history holds %d messages, want %d", len(messages), len(said))
	}
	for i, want := range said {
		if got := messages[i].GetBody(); got != want {
			t.Errorf("history[%d] = %q, want %q", i, got, want)
		}
		if got := messages[i].GetSeq(); got != int64(i+1) {
			t.Errorf("history[%d] sits at %d, want %d", i, got, i+1)
		}
	}
}

func TestAReconnectLosesNothing(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	room := alice.createRoom(t, "e2e reconnect")
	bob.joinRoom(t, room.GetId())

	as := alice.connect(t)
	as.subscribe(room.GetId(), 0)

	bs := bob.connect(t)
	bs.subscribe(room.GetId(), 0)

	as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: "before the drop"})
	as.expect(typeAck)
	as.expect(typeMessage)

	seen := bs.expect(typeMessage).Message.Seq

	if err := bs.conn.CloseNow(); err != nil {
		t.Fatalf("drop bob's connection: %v", err)
	}

	missed := []string{"while away one", "while away two"}
	for _, body := range missed {
		as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: body})
		as.expect(typeAck)
		as.expect(typeMessage)
	}

	reconnected := bob.connect(t)
	if current := reconnected.subscribe(room.GetId(), seen); current != seen+int64(len(missed)) {
		t.Errorf("the room reports position %d, want %d", current, seen+int64(len(missed)))
	}

	for _, want := range missed {
		if got := reconnected.expect(typeMessage); got.Message.Body != want {
			t.Fatalf("caught up with %q, want %q", got.Message.Body, want)
		}
	}

	as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: "after the return"})
	as.expect(typeAck)

	if got := reconnected.expect(typeMessage); got.Message.Body != "after the return" {
		t.Errorf("live delivery resumed with %q", got.Message.Body)
	}
}

func TestAStrangerIsRefused(t *testing.T) {
	alice, stranger := newUser(t), newUser(t)

	room := alice.createRoom(t, "e2e private-ish")

	ss := stranger.connect(t)

	ss.send(inbound{Type: typeSubscribe, RoomID: room.GetId()})
	if refusal := ss.expect(typeError); refusal.Code != errorNotAMember {
		t.Errorf("subscribe refused with %q, want %q", refusal.Code, errorNotAMember)
	}

	ss.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: "let me in"})
	if refusal := ss.expect(typeError); refusal.Code != errorNotAMember {
		t.Errorf("send refused with %q, want %q", refusal.Code, errorNotAMember)
	}

	_, err := stranger.chat.ListMessages(context.Background(),
		connect.NewRequest(&chatv1.ListMessagesRequest{RoomId: room.GetId()}))
	if err == nil {
		t.Fatal("a stranger read the room's history")
	}
	if code := connect.CodeOf(err); code != connect.CodePermissionDenied && code != connect.CodeNotFound {
		t.Errorf("ListMessages refused with %s", code)
	}
}

func TestASocketWithoutATokenIsRefused(t *testing.T) {
	_, err := ws.Dial(context.Background(), gatewaySocketURL, ws.DialOptions{
		Subprotocols: []string{subprotocol},
	})
	if err == nil {
		t.Fatal("a socket opened through the gateway with no token")
	}
}

func TestAResendIsNotASecondMessage(t *testing.T) {
	alice, bob := newUser(t), newUser(t)

	room := alice.createRoom(t, "e2e resend")
	bob.joinRoom(t, room.GetId())

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)

	frame := inbound{Type: typeSend, RoomID: room.GetId(), ClientID: "alice-resend", Body: "once"}

	as.send(frame)
	first := as.expect(typeAck)
	as.expect(typeMessage)
	bs.expect(typeMessage)

	as.send(frame)
	if again := as.expect(typeAck); again.Seq != first.Seq {
		t.Errorf("the resend was acknowledged at %d, want the original %d", again.Seq, first.Seq)
	}

	as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: "twice"})
	as.expect(typeAck)

	if got := bs.expect(typeMessage); got.Message.Body != "twice" {
		t.Errorf("bob's next frame was %q — the resend was delivered again", got.Message.Body)
	}

	history, err := alice.chat.ListMessages(context.Background(),
		connect.NewRequest(&chatv1.ListMessagesRequest{RoomId: room.GetId()}))
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if n := len(history.Msg.GetMessages()); n != 2 {
		t.Errorf("history holds %d messages, want 2", n)
	}
}

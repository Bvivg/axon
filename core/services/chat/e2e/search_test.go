//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
)

func TestSearchFindsMessagesOnlyForMembers(t *testing.T) {
	alice, bob, carol := newUser(t), newUser(t), newUser(t)
	room := alice.createRoom(t, "e2e search")
	bob.joinRoom(t, room.GetId())

	as := alice.connect(t)
	as.subscribe(room.GetId(), 0)
	for _, body := range []string{"Release notes are out", "lunch?", "the release is tomorrow"} {
		as.send(inbound{Type: typeSend, RoomID: room.GetId(), Body: body})
		as.expect(typeAck)
		as.expect(typeMessage)
	}

	ctx := context.Background()
	found, err := bob.chat.SearchMessages(ctx, connect.NewRequest(&chatv1.SearchMessagesRequest{
		RoomId: room.GetId(), Query: "  RELEASE ",
	}))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var bodies []string
	for _, m := range found.Msg.GetMessages() {
		bodies = append(bodies, m.GetBody())
	}
	if len(bodies) != 2 || bodies[0] != "the release is tomorrow" || bodies[1] != "Release notes are out" {
		t.Fatalf("bob found %q, want both release messages newest first", bodies)
	}

	cases := []struct {
		name  string
		who   chatv1connect.ChatServiceClient
		query string
		want  connect.Code
	}{
		{"stranger", carol.chat, "release", connect.CodeNotFound},
		{"short query", bob.chat, " r ", connect.CodeInvalidArgument},
		{"anonymous", chatv1connect.NewChatServiceClient(&http.Client{Transport: forwardedFor{addr: "10.250.0.2"}}, gatewayURL), "release", connect.CodeUnauthenticated},
	}
	for _, tc := range cases {
		_, err := tc.who.SearchMessages(ctx, connect.NewRequest(&chatv1.SearchMessagesRequest{RoomId: room.GetId(), Query: tc.query}))
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) || connectErr.Code() != tc.want {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

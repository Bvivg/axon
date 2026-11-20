//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
)

type albumPayload struct {
	Items []struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	} `json:"items"`
}

func TestPhotosSentTogetherArriveAsOneAlbumAndPartOfItCanBeForwarded(t *testing.T) {
	alice, bob, carol := newUser(t), newUser(t), newUser(t)
	room := alice.createRoom(t, "e2e album")
	bob.joinRoom(t, room.GetId())
	elsewhere := alice.createRoom(t, "e2e album forward")
	carol.joinRoom(t, elsewhere.GetId())

	photo := photoWithLocation(t)
	var ids []string
	for range 3 {
		ids = append(ids, alice.uploadInParts(t, "media", "photo.jpg", photo).UploadID)
	}

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)
	as.send(inbound{Type: typeSend, RoomID: room.GetId(), ClientID: uuid.NewString(), UploadIDs: ids, Body: "the set"})

	got := bs.expect(typeMessage).Message
	var album albumPayload
	if err := json.Unmarshal(got.Payload, &album); err != nil {
		t.Fatalf("album payload: %v", err)
	}
	if got.Kind != "album" || got.Body != "the set" || len(album.Items) != 3 || album.Items[0].Kind != "image" {
		t.Fatalf("bob got %s %q with %d items, want an album of three photos with the caption", got.Kind, got.Body, len(album.Items))
	}

	cs := carol.connect(t)
	cs.subscribe(elsewhere.GetId(), 0)
	if _, err := alice.chat.ForwardMessages(context.Background(), connect.NewRequest(&chatv1.ForwardMessagesRequest{
		MessageIds: []string{got.ID},
		RoomIds:    []string{elsewhere.GetId()},
		ClientId:   uuid.NewString(),
		Parts:      []*chatv1.ForwardPart{{MessageId: got.ID, Positions: []int32{0, 2}}},
	})); err != nil {
		t.Fatalf("forward part of the album: %v", err)
	}

	forwarded := cs.expect(typeMessage).Message
	var part albumPayload
	if err := json.Unmarshal(forwarded.Payload, &part); err != nil {
		t.Fatalf("forwarded payload: %v", err)
	}
	if forwarded.Kind != "album" || len(part.Items) != 2 || forwarded.Body != "" || forwarded.ForwardedFromID != got.ID {
		t.Fatalf("carol got %s %q with %d items, want two photos forwarded without the caption", forwarded.Kind, forwarded.Body, len(part.Items))
	}
}

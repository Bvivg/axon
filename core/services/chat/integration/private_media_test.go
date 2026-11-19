//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestPrivateMediaRequiresVisibleMembership(t *testing.T) {
	repo := newRepo(t)
	room, alice, members := newGroup(t, repo, 2)
	bob, carol := members[0], members[1]
	key := alice.String() + "/" + uuid.NewString() + ".txt"
	payload, _ := json.Marshal(domain.AttachmentPayload{URL: "http://old-storage/chat-attachments/" + key, Filename: "private.txt", Mime: "text/plain", SizeBytes: 7})
	upload, err := repo.CreateUpload(t.Context(), domain.Upload{ID: uuid.New(), UploaderID: alice, Kind: domain.MessageKindAttachment, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	check := func(user uuid.UUID, want bool) {
		t.Helper()
		got, err := repo.CanReadObject(t.Context(), user, key)
		if err != nil || got != want {
			t.Fatalf("access=%v want=%v err=%v", got, want, err)
		}
	}
	check(alice, true)
	check(bob, false)
	check(uuid.New(), false)
	message, _, err := repo.AppendMessage(t.Context(), domain.Message{ID: uuid.New(), RoomID: room.ID, AuthorID: alice, Kind: domain.MessageKindAttachment, Payload: payload, UploadID: &upload.ID})
	if err != nil {
		t.Fatal(err)
	}
	check(alice, true)
	check(bob, true)
	if err := repo.HideRoom(t.Context(), room.ID, bob); err != nil {
		t.Fatal(err)
	}
	check(bob, false)
	if err := repo.RemoveMember(t.Context(), room.ID, carol); err != nil {
		t.Fatal(err)
	}
	check(carol, false)
	if _, err := repo.DeleteMessage(t.Context(), message.ID); err != nil {
		t.Fatal(err)
	}
	check(alice, false)
}

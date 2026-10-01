//go:build integration

package integration

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestHistoryCarriesTheReplyPreview(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "quotes")

	long := strings.Repeat("ab", 100)
	original := say(t, r, room.ID, owner, long, "")

	replyTo := original.ID
	if _, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: "answer", ReplyToID: &replyTo,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	history, _, err := r.ListMessages(t.Context(), room.ID, owner, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	reply := history[1]
	if reply.ReplyTo == nil {
		t.Fatal("the reply came back without a preview")
	}
	if reply.ReplyTo.AuthorID != owner || reply.ReplyTo.Kind != domain.MessageKindText ||
		reply.ReplyTo.Body != long[:domain.ReplyPreviewLength] {
		t.Errorf("preview = %+v, want the first %d characters of the original", reply.ReplyTo, domain.ReplyPreviewLength)
	}

	if _, err := r.DeleteMessage(t.Context(), original.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	after, err := r.MessageByID(t.Context(), reply.ID)
	if err != nil {
		t.Fatalf("MessageByID: %v", err)
	}
	if after.ReplyTo == nil || !after.ReplyTo.Deleted || after.ReplyTo.Body != "" {
		t.Errorf("preview after delete = %+v, want deleted and empty", after.ReplyTo)
	}
}

func TestAForwardKeepsTheOriginalAuthor(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "forwards")
	original := say(t, r, room.ID, owner, "first said here", "")

	origin := uuid.New()
	from := original.ID
	stored, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Body: original.Body,
		ForwardedFromID: &from, ForwardOriginAuthorID: &origin,
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if stored.ForwardOriginAuthorID == nil || *stored.ForwardOriginAuthorID != origin {
		t.Errorf("origin author = %v, want %s", stored.ForwardOriginAuthorID, origin)
	}
}

func TestEditAndDeleteRewriteTheStoredMessage(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "changes")
	sent := say(t, r, room.ID, owner, "helo", "")

	edited, err := r.EditMessage(t.Context(), sent.ID, "hello")
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if edited.Body != "hello" || edited.EditedAt == nil || edited.Seq != sent.Seq {
		t.Errorf("edited = %+v, want the new body, a timestamp and the same position", edited)
	}

	deleted, err := r.DeleteMessage(t.Context(), sent.ID)
	if err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if deleted.Body != "" || deleted.DeletedAt == nil || string(deleted.Payload) != "{}" {
		t.Errorf("deleted = %+v, want an emptied message with deleted_at", deleted)
	}

	if _, err := r.EditMessage(t.Context(), sent.ID, "back"); !errors.Is(err, domain.ErrMessageDeleted) {
		t.Errorf("edit after delete = %v, want ErrMessageDeleted", err)
	}
	if _, err := r.DeleteMessage(t.Context(), sent.ID); !errors.Is(err, domain.ErrMessageDeleted) {
		t.Errorf("second delete = %v, want ErrMessageDeleted", err)
	}
}

func TestADeletedPhotoLeavesTheMediaList(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "gallery")

	photo, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Kind: domain.MessageKindImage,
		Payload: json.RawMessage(`{"url":"http://minio/p.jpg","width":1,"height":1}`),
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if _, err := r.DeleteMessage(t.Context(), photo.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	media, _, err := r.ListMessages(t.Context(), room.ID, owner, domain.Page{
		Limit: 10, Kinds: []domain.MessageKind{domain.MessageKindImage},
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(media) != 0 {
		t.Errorf("media list holds %d items after the photo was deleted, want 0", len(media))
	}

	all, _, err := r.ListMessages(t.Context(), room.ID, owner, domain.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(all) != 1 || all[0].DeletedAt == nil {
		t.Errorf("history = %+v, want the deleted photo kept as a placeholder", all)
	}
}

func TestAMessageCannotPointAtAMissingUpload(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "uploads")

	missing := uuid.New()
	_, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: room.ID, AuthorID: owner, Kind: domain.MessageKindImage, UploadID: &missing,
		Payload: json.RawMessage(`{"url":"http://minio/p.jpg","width":1,"height":1}`),
	})
	if !errors.Is(err, domain.ErrUploadNotFound) {
		t.Fatalf("err = %v, want ErrUploadNotFound", err)
	}
}

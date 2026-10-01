package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

func (h *harness) say(t *testing.T, roomID, author uuid.UUID, body string) domain.Message {
	t.Helper()

	m, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: roomID, AuthorID: author, Body: body})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	return m
}

func TestEditingOwnTextMarksItEdited(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	sent := h.say(t, room.ID, owner, "helo")

	edited, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: sent.ID, UserID: owner, Body: " hello "})
	if err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if edited.Body != "hello" || edited.EditedAt == nil {
		t.Errorf("edited = %q, edited_at %v, want trimmed body and a timestamp", edited.Body, edited.EditedAt)
	}

	same, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: sent.ID, UserID: owner, Body: "hello"})
	if err != nil {
		t.Fatalf("EditMessage unchanged: %v", err)
	}
	if !same.EditedAt.Equal(*edited.EditedAt) {
		t.Error("an unchanged edit touched edited_at")
	}
}

func TestOnlyTheAuthorEditsOrDeletes(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	other := uuid.New()
	if _, _, err := h.svc.JoinRoom(t.Context(), service.JoinRoomInput{RoomID: room.ID, UserID: other}); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	sent := h.say(t, room.ID, owner, "mine")

	if _, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: sent.ID, UserID: other, Body: "yours"}); !errors.Is(err, domain.ErrNotMessageAuthor) {
		t.Errorf("edit by other = %v, want ErrNotMessageAuthor", err)
	}
	if _, err := h.svc.DeleteMessage(t.Context(), sent.ID, other); !errors.Is(err, domain.ErrNotMessageAuthor) {
		t.Errorf("delete by other = %v, want ErrNotMessageAuthor", err)
	}
	if _, err := h.svc.DeleteMessage(t.Context(), sent.ID, uuid.New()); !errors.Is(err, domain.ErrMessageNotFound) {
		t.Errorf("delete by outsider = %v, want ErrMessageNotFound", err)
	}
}

func TestEditRefusesWhatCannotChange(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	original := h.say(t, room.ID, owner, "original")

	forwarded, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, ForwardedFromID: original.ID.String(),
	})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if _, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: forwarded.ID, UserID: owner, Body: "x"}); !errors.Is(err, domain.ErrMessageNotEditable) {
		t.Errorf("edit forwarded = %v, want ErrMessageNotEditable", err)
	}

	if _, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: original.ID, UserID: owner, Body: "  "}); err == nil {
		t.Error("an empty edit of a text message succeeded, want a validation error")
	}
}

func TestDeleteClearsContentAndBlocksFurtherChanges(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	sent := h.say(t, room.ID, owner, "secret")

	deleted, err := h.svc.DeleteMessage(t.Context(), sent.ID, owner)
	if err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}
	if !deleted.Deleted() || deleted.Body != "" {
		t.Errorf("deleted = %+v, want an empty body and deleted_at", deleted)
	}

	if _, err := h.svc.DeleteMessage(t.Context(), sent.ID, owner); !errors.Is(err, domain.ErrMessageDeleted) {
		t.Errorf("second delete = %v, want ErrMessageDeleted", err)
	}
	if _, err := h.svc.EditMessage(t.Context(), service.EditInput{MessageID: sent.ID, UserID: owner, Body: "back"}); !errors.Is(err, domain.ErrMessageDeleted) {
		t.Errorf("edit after delete = %v, want ErrMessageDeleted", err)
	}
	if _, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, ForwardedFromID: sent.ID.String(),
	}); err == nil {
		t.Error("forwarding a deleted message succeeded")
	}
}

func TestRepliesCarryAPreviewAndForwardsKeepTheFirstAuthor(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	other := uuid.New()
	if _, _, err := h.svc.JoinRoom(t.Context(), service.JoinRoomInput{RoomID: room.ID, UserID: other}); err != nil {
		t.Fatalf("JoinRoom: %v", err)
	}
	original := h.say(t, room.ID, owner, "the original words")

	reply, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: other, Body: "agreed", ReplyToID: original.ID.String(),
	})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.ReplyTo == nil || reply.ReplyTo.AuthorID != owner || reply.ReplyTo.Body != "the original words" {
		t.Errorf("reply preview = %+v, want the original's author and text", reply.ReplyTo)
	}

	first, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: other, ForwardedFromID: original.ID.String(),
	})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	second, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, ForwardedFromID: first.ID.String(),
	})
	if err != nil {
		t.Fatalf("forward again: %v", err)
	}
	if second.ForwardOriginAuthorID == nil || *second.ForwardOriginAuthorID != owner {
		t.Errorf("origin author = %v, want the first author %s", second.ForwardOriginAuthorID, owner)
	}
}

func TestAForwardCarriesTheOriginalWordsNotTheSendersOwn(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "general")
	original := h.say(t, room.ID, owner, "what I actually said")

	forwarded, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: room.ID, AuthorID: owner, Body: "words put in my mouth", ForwardedFromID: original.ID.String(),
	})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if forwarded.Body != original.Body || forwarded.Kind != original.Kind {
		t.Errorf("forwarded = %q (%s), want the original %q", forwarded.Body, forwarded.Kind, original.Body)
	}
}

package service_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

func imagePayload(t *testing.T) json.RawMessage {
	t.Helper()

	raw, err := json.Marshal(domain.ImagePayload{
		URL:          "http://minio/chat-attachments/a.jpg",
		ThumbnailURL: "http://minio/chat-attachments/a.thumb.jpg",
		Width:        1600,
		Height:       1200,
		SizeBytes:    120_000,
		Mime:         "image/jpeg",
	})
	if err != nil {
		t.Fatalf("marshal image payload: %v", err)
	}
	return raw
}

func TestAnUploadBecomesTheMessageItIsSentAs(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "photos")

	upload, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindImage, imagePayload(t))
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}

	message, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID:   room.ID,
		AuthorID: owner,
		Body:     "the view",
		UploadID: upload.ID.String(),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if message.Kind != domain.MessageKindImage {
		t.Errorf("kind = %q, want %q", message.Kind, domain.MessageKindImage)
	}
	if message.Body != "the view" {
		t.Errorf("body = %q, want the caption", message.Body)
	}

	var got domain.ImagePayload
	if err := json.Unmarshal(message.Payload, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if got.Width != 1600 || got.Height != 1200 || got.URL == "" {
		t.Errorf("payload = %+v, want the uploaded image", got)
	}
}

func TestSendingMediaNeedsAnUploadOfYourOwn(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "photos")

	stranger := uuid.New()
	theirs, err := h.svc.RecordUpload(t.Context(), stranger, domain.MessageKindImage, imagePayload(t))
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	mine, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindImage, imagePayload(t))
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}

	for name, tc := range map[string]struct {
		in   service.SendInput
		want error
	}{
		"someone else's upload": {
			in:   service.SendInput{Kind: "image", UploadID: theirs.ID.String()},
			want: domain.ErrUploadNotFound,
		},
		"an upload that does not exist": {
			in:   service.SendInput{UploadID: uuid.NewString()},
			want: domain.ErrUploadNotFound,
		},
		"an upload id that is not an id": {
			in:   service.SendInput{UploadID: "not-an-id"},
			want: domain.ErrUploadNotFound,
		},
		"a media kind with no upload": {
			in:   service.SendInput{Kind: "image"},
			want: domain.ErrUploadRequired,
		},
		"a file kind with no upload": {
			in:   service.SendInput{Kind: "attachment"},
			want: domain.ErrUploadRequired,
		},
		"an upload sent as another kind": {
			in:   service.SendInput{Kind: "video", UploadID: mine.ID.String()},
			want: domain.ErrUploadKindMismatch,
		},
	} {
		t.Run(name, func(t *testing.T) {
			tc.in.RoomID = room.ID
			tc.in.AuthorID = owner

			_, _, err := h.svc.Send(t.Context(), tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Send = %v, want %v", err, tc.want)
			}
		})
	}

	if n := h.events.count(); n != 0 {
		t.Errorf("%d messages announced, want none", n)
	}
}

func TestAForwardedPhotoCarriesTheOriginalPayload(t *testing.T) {
	h := newHarness(t)
	source, owner := h.openRoom(t, "source")
	target, err := h.svc.CreateRoom(t.Context(), service.CreateRoomInput{Name: "target", UserID: owner})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	upload, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindImage, imagePayload(t))
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	original, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID: source.ID, AuthorID: owner, UploadID: upload.ID.String(),
	})
	if err != nil {
		t.Fatalf("Send original: %v", err)
	}

	forwarded, _, err := h.svc.Send(t.Context(), service.SendInput{
		RoomID:          target.ID,
		AuthorID:        owner,
		Kind:            "image",
		ForwardedFromID: original.ID.String(),
	})
	if err != nil {
		t.Fatalf("Send forward: %v", err)
	}

	if string(forwarded.Payload) != string(original.Payload) {
		t.Errorf("forwarded payload = %s, want %s", forwarded.Payload, original.Payload)
	}
	if forwarded.ForwardedFromID == nil || *forwarded.ForwardedFromID != original.ID {
		t.Errorf("forwarded_from_id = %v, want %s", forwarded.ForwardedFromID, original.ID)
	}
}

func TestRecordUploadRefusesWhatIsNotAnUpload(t *testing.T) {
	h := newHarness(t)

	for name, tc := range map[string]struct {
		kind    domain.MessageKind
		payload json.RawMessage
	}{
		"text":               {kind: domain.MessageKindText, payload: json.RawMessage(`{}`)},
		"system":             {kind: domain.MessageKindSystem, payload: json.RawMessage(`{"event":"x"}`)},
		"image with no size": {kind: domain.MessageKindImage, payload: json.RawMessage(`{"url":"u"}`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.svc.RecordUpload(t.Context(), uuid.New(), tc.kind, tc.payload); err == nil {
				t.Fatal("RecordUpload succeeded, want an error")
			}
		})
	}
}

func TestHistoryCanBeNarrowedToMedia(t *testing.T) {
	h := newHarness(t)
	room, owner := h.openRoom(t, "mixed")

	if _, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, Body: "hi"}); err != nil {
		t.Fatalf("Send text: %v", err)
	}
	upload, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindImage, imagePayload(t))
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	if _, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, UploadID: upload.ID.String()}); err != nil {
		t.Fatalf("Send image: %v", err)
	}

	history, err := h.svc.ListMessages(t.Context(), room.ID, owner, domain.Page{
		Kinds: []domain.MessageKind{domain.MessageKindImage, domain.MessageKindVideo},
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(history.Messages) != 1 || history.Messages[0].Kind != domain.MessageKindImage {
		t.Fatalf("history = %+v, want the one image", history.Messages)
	}
}

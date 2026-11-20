package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

func (h *harness) photo(t *testing.T, owner uuid.UUID, n int) domain.Upload {
	t.Helper()
	payload, _ := json.Marshal(domain.ImagePayload{
		URL: fmt.Sprintf("http://media/p%d.jpg", n), ThumbnailURL: fmt.Sprintf("http://media/p%d.thumb.jpg", n),
		Width: 800, Height: 600, SizeBytes: 1000 + int64(n), Mime: "image/jpeg",
	})
	upload, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindImage, payload)
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	return upload
}

func albumOf(t *testing.T, m domain.Message) domain.AlbumPayload {
	t.Helper()
	var album domain.AlbumPayload
	if err := json.Unmarshal(m.Payload, &album); err != nil {
		t.Fatalf("album payload: %v", err)
	}
	return album
}

func TestSeveralPhotosGoAsOneAlbumInTheOrderSent(t *testing.T) {
	h := newHarness(t)
	room, owner, _ := h.group(t, 1)
	ids := []string{}
	for n := range 3 {
		ids = append(ids, h.photo(t, owner, n).ID.String())
	}

	m, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, UploadIDs: ids, Body: "trip"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	album := albumOf(t, m)
	if m.Kind != domain.MessageKindAlbum || m.Body != "trip" || len(album.Items) != 3 || len(m.AlbumUploads) != 3 {
		t.Fatalf("message = %+v, want an album of three with the caption", m)
	}
	for i, item := range album.Items {
		if item.Kind != domain.MessageKindImage || item.URL != fmt.Sprintf("http://media/p%d.jpg", i) {
			t.Errorf("item %d = %+v, want photo %d", i, item, i)
		}
		if m.AlbumUploads[i].String() != ids[i] {
			t.Errorf("upload %d = %s, want %s", i, m.AlbumUploads[i], ids[i])
		}
	}
}

func TestAlbumsRefuseWhatTheyCannotHold(t *testing.T) {
	h := newHarness(t)
	room, owner, members := h.group(t, 1)
	photo := h.photo(t, owner, 1).ID.String()
	other := h.photo(t, members[0], 2).ID.String()
	voicePayload, _ := json.Marshal(domain.VoicePayload{DurationMS: 1000, URL: "http://media/v.m4a"})
	voice, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindVoice, voicePayload)
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	eleven := []string{}
	for n := range domain.MaxAlbumItems + 1 {
		eleven = append(eleven, h.photo(t, owner, 10+n).ID.String())
	}

	invalid := func(err error) bool { _, ok := domain.AsValidationError(err); return ok }
	tests := map[string]struct {
		ids  []string
		want func(error) bool
	}{
		"more than ten":        {ids: eleven, want: invalid},
		"the same photo twice": {ids: []string{photo, photo}, want: invalid},
		"a voice note inside":  {ids: []string{photo, voice.ID.String()}, want: invalid},
		"someone else's photo": {ids: []string{photo, other}, want: func(err error) bool { return errors.Is(err, domain.ErrUploadNotFound) }},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, UploadIDs: tt.ids})
			if !tt.want(err) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	single, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, UploadIDs: []string{photo}})
	if err != nil || single.Kind != domain.MessageKindImage || single.UploadID == nil {
		t.Fatalf("one upload id = %+v, %v; want an ordinary photo", single, err)
	}
}

func TestForwardingAWholeAlbumOrSomeOfIt(t *testing.T) {
	h := newHarness(t)
	room, owner, _ := h.group(t, 1)
	filePayload := domain.AttachmentPayload{
		URL: "http://media/notes.txt", Filename: "notes.txt", SizeBytes: 42, Mime: "text/plain",
	}
	payload, _ := json.Marshal(filePayload)
	file, err := h.svc.RecordUpload(t.Context(), owner, domain.MessageKindAttachment, payload)
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}
	ids := []string{h.photo(t, owner, 0).ID.String(), file.ID.String(), h.photo(t, owner, 2).ID.String()}
	album, _, err := h.svc.Send(t.Context(), service.SendInput{RoomID: room.ID, AuthorID: owner, UploadIDs: ids, Body: "trip"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	target, _, _ := h.group(t, 1)
	if _, err := h.store.AddMember(t.Context(), target.ID, domain.Member{UserID: owner}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}

	forward := func(parts []service.ForwardPart) domain.Message {
		t.Helper()
		sent, err := h.svc.ForwardMessages(t.Context(), service.ForwardInput{
			AuthorID: owner, MessageIDs: []string{album.ID.String()}, RoomIDs: []string{target.ID.String()}, Parts: parts,
		})
		if err != nil || len(sent) != 1 {
			t.Fatalf("ForwardMessages = %v, %v", sent, err)
		}
		return sent[0].Message
	}

	whole := forward(nil)
	if whole.Kind != domain.MessageKindAlbum || whole.Body != "trip" || len(albumOf(t, whole).Items) != 3 || len(whole.AlbumUploads) != 3 {
		t.Fatalf("whole forward = %+v, want the album with its uploads", whole)
	}
	groupedFile := albumOf(t, whole).Items[1]
	if groupedFile.Kind != domain.MessageKindAttachment || groupedFile.Filename != filePayload.Filename ||
		groupedFile.Mime != filePayload.Mime || whole.AlbumUploads[1] != file.ID {
		t.Fatalf("grouped file = %+v, want the original file at position 1", groupedFile)
	}

	two := forward([]service.ForwardPart{{MessageID: album.ID.String(), Positions: []int{2, 0}}})
	items := albumOf(t, two).Items
	if two.Kind != domain.MessageKindAlbum || two.Body != "" || len(items) != 2 || items[0].URL != "http://media/p2.jpg" ||
		two.AlbumUploads[1].String() != ids[0] {
		t.Fatalf("two picked = %+v, want an album of photos 2 and 0", two)
	}

	one := forward([]service.ForwardPart{{MessageID: album.ID.String(), Positions: []int{1}}})
	if one.Kind != domain.MessageKindAttachment || one.UploadID == nil || *one.UploadID != file.ID {
		t.Fatalf("one picked = %+v, want a standalone file backed by its upload", one)
	}
	var forwardedFile domain.AttachmentPayload
	if err := json.Unmarshal(one.Payload, &forwardedFile); err != nil || forwardedFile.Filename != filePayload.Filename {
		t.Fatalf("forwarded file = %+v, %v; want the original filename", forwardedFile, err)
	}

	_, err = h.svc.ForwardMessages(t.Context(), service.ForwardInput{
		AuthorID: owner, MessageIDs: []string{album.ID.String()}, RoomIDs: []string{target.ID.String()},
		Parts: []service.ForwardPart{{MessageID: album.ID.String(), Positions: []int{3}}},
	})
	if _, ok := domain.AsValidationError(err); !ok {
		t.Fatalf("a position past the album: err = %v, want a validation error", err)
	}
}

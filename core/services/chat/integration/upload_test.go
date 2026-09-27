//go:build integration

package integration

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestAnUploadIsReadBackAsItWasRecorded(t *testing.T) {
	r := newRepo(t)

	payload := json.RawMessage(`{"url":"http://minio/a.jpg","thumbnail_url":"http://minio/a.thumb.jpg","width":10,"height":20,"size_bytes":300,"mime":"image/jpeg"}`)
	stored, err := r.CreateUpload(t.Context(), domain.Upload{
		ID:         uuid.New(),
		UploaderID: uuid.New(),
		Kind:       domain.MessageKindImage,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}
	if stored.CreatedAt.IsZero() {
		t.Error("created_at was not set")
	}

	got, err := r.UploadByID(t.Context(), stored.ID)
	if err != nil {
		t.Fatalf("UploadByID: %v", err)
	}
	if got.Kind != domain.MessageKindImage || got.UploaderID != stored.UploaderID {
		t.Errorf("upload = %+v, want %+v", got, stored)
	}

	var want, have map[string]any
	_ = json.Unmarshal(payload, &want)
	_ = json.Unmarshal(got.Payload, &have)
	if have["width"] != want["width"] || have["url"] != want["url"] {
		t.Errorf("payload = %s, want %s", got.Payload, payload)
	}
}

func TestAMissingUploadIsReportedAsSuch(t *testing.T) {
	r := newRepo(t)

	if _, err := r.UploadByID(t.Context(), uuid.New()); !errors.Is(err, domain.ErrUploadNotFound) {
		t.Fatalf("UploadByID = %v, want ErrUploadNotFound", err)
	}
}

func TestAnUploadOfAnUnknownKindIsRefusedByTheSchema(t *testing.T) {
	r := newRepo(t)

	_, err := r.CreateUpload(t.Context(), domain.Upload{
		ID:         uuid.New(),
		UploaderID: uuid.New(),
		Kind:       domain.MessageKindText,
		Payload:    json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("a text upload was stored")
	}
}

func TestHistoryNarrowsToTheKindsAskedFor(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "mixed")

	say(t, r, room.ID, owner, "just words", "")

	for _, kind := range []domain.MessageKind{domain.MessageKindImage, domain.MessageKindAttachment} {
		if _, _, err := r.AppendMessage(t.Context(), domain.Message{
			ID:       uuid.New(),
			RoomID:   room.ID,
			AuthorID: owner,
			Kind:     kind,
			Payload:  json.RawMessage(`{"url":"http://minio/x"}`),
		}); err != nil {
			t.Fatalf("AppendMessage(%s): %v", kind, err)
		}
	}

	for name, tc := range map[string]struct {
		kinds []domain.MessageKind
		want  int
	}{
		"everything":  {kinds: nil, want: 3},
		"photos only": {kinds: []domain.MessageKind{domain.MessageKindImage, domain.MessageKindVideo}, want: 1},
		"files only":  {kinds: []domain.MessageKind{domain.MessageKindAttachment}, want: 1},
		"voice only":  {kinds: []domain.MessageKind{domain.MessageKindVoice}, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := r.ListMessages(t.Context(), room.ID, owner, domain.Page{Limit: 10, Kinds: tc.kinds})
			if err != nil {
				t.Fatalf("ListMessages: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("got %d messages, want %d", len(got), tc.want)
			}
			for _, m := range got {
				if len(tc.kinds) > 0 && m.Kind != tc.kinds[0] && (len(tc.kinds) < 2 || m.Kind != tc.kinds[1]) {
					t.Errorf("a %s message slipped through", m.Kind)
				}
			}
		})
	}
}

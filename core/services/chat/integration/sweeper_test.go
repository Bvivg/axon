//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/repository"
)

type storedUpload struct {
	upload domain.Upload
	urls   []string
}

func putUpload(t *testing.T, r *repository.Repository, age time.Duration) storedUpload {
	t.Helper()

	uploader := uuid.New()
	mainKey := attachment.NewObjectKey(uploader, "photo.jpg")
	thumbKey := attachment.NewObjectKey(uploader, "photo.thumb.jpg")
	for _, key := range []string{mainKey, thumbKey} {
		if err := attachmentStore.Put(t.Context(), attachment.Object{Key: key, ContentType: "image/jpeg", Data: []byte("jpeg")}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	urls := []string{attachmentURLs.URL(mainKey), attachmentURLs.URL(thumbKey)}
	payload, _ := json.Marshal(map[string]any{
		"url": urls[0], "thumbnail_url": urls[1], "width": 1, "height": 1, "mime": "image/jpeg",
	})

	upload, err := r.CreateUpload(t.Context(), domain.Upload{
		ID: uuid.New(), UploaderID: uploader, Kind: domain.MessageKindImage, Payload: payload,
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE uploads SET created_at = now() - make_interval(secs => $2) WHERE id = $1`,
		upload.ID, age.Seconds()); err != nil {
		t.Fatalf("age upload: %v", err)
	}
	return storedUpload{upload: upload, urls: urls}
}

func sendUpload(t *testing.T, r *repository.Repository, roomID, author uuid.UUID, u storedUpload) domain.Message {
	t.Helper()

	id := u.upload.ID
	m, _, err := r.AppendMessage(t.Context(), domain.Message{
		ID: uuid.New(), RoomID: roomID, AuthorID: author, Kind: domain.MessageKindImage,
		Payload: u.upload.Payload, UploadID: &id,
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	return m
}

func objectsExist(t *testing.T, urls []string) bool {
	t.Helper()

	present := 0
	for _, url := range urls {
		if fetchAttachment(t, url).status == http.StatusOK {
			present++
		}
	}
	if present != 0 && present != len(urls) {
		t.Fatalf("%d of %d objects are present, want all or none", present, len(urls))
	}
	return present == len(urls)
}

func TestTheSweeperRemovesOnlyOldUploadsNobodyShows(t *testing.T) {
	r := newRepo(t)
	room, owner := newRoom(t, r, "sweep")

	day := 24 * time.Hour
	unsent := putUpload(t, r, 2*day)
	fresh := putUpload(t, r, time.Minute)
	shown := putUpload(t, r, 2*day)
	unsentAfterDelete := putUpload(t, r, 2*day)
	sharedByAForward := putUpload(t, r, 2*day)

	sendUpload(t, r, room.ID, owner, shown)

	deleted := sendUpload(t, r, room.ID, owner, unsentAfterDelete)
	if _, err := r.DeleteMessage(t.Context(), deleted.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	original := sendUpload(t, r, room.ID, owner, sharedByAForward)
	sendUpload(t, r, room.ID, owner, sharedByAForward)
	if _, err := r.DeleteMessage(t.Context(), original.ID); err != nil {
		t.Fatalf("DeleteMessage: %v", err)
	}

	sweeper, err := attachment.NewSweeper(attachment.SweeperConfig{
		Objects:     attachmentStore,
		Uploads:     r,
		URLs:        attachmentURLs,
		Interval:    time.Hour,
		OrphanAfter: day,
		Logger:      logger.Discard(),
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}

	swept, err := sweeper.Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept < 2 {
		t.Errorf("swept %d uploads, want at least the 2 orphans made here", swept)
	}

	for name, tc := range map[string]struct {
		u    storedUpload
		kept bool
	}{
		"unsent":                 {unsent, false},
		"fresh":                  {fresh, true},
		"shown":                  {shown, true},
		"deleted for everyone":   {unsentAfterDelete, false},
		"still shown by forward": {sharedByAForward, true},
	} {
		_, err := r.UploadByID(t.Context(), tc.u.upload.ID)
		if kept := err == nil; kept != tc.kept {
			t.Errorf("%s: row kept = %v, want %v (err %v)", name, kept, tc.kept, err)
		}
		if kept := objectsExist(t, tc.u.urls); kept != tc.kept {
			t.Errorf("%s: objects kept = %v, want %v", name, kept, tc.kept)
		}
	}

	again, err := sweeper.Sweep(t.Context())
	if err != nil || again != 0 {
		t.Errorf("second sweep = %d, %v, want nothing left to do", again, err)
	}
}

func TestTheSweeperOnlyTouchesItsOwnBucket(t *testing.T) {
	sweeper, err := attachment.NewSweeper(attachment.SweeperConfig{
		Objects:     attachmentStore,
		Uploads:     repository.New(pool),
		URLs:        attachmentURLs,
		Interval:    time.Hour,
		OrphanAfter: time.Hour,
		Logger:      logger.Discard(),
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}

	keys := sweeper.ObjectKeys(json.RawMessage(`{
		"url": "` + attachmentURLs.URL("u/a.jpg") + `",
		"poster_url": "https://elsewhere.example/chat-attachments/u/b.jpg",
		"thumbnail_url": "` + attachmentURLs.URL("../escape") + `",
		"width": 10
	}`))
	if len(keys) != 1 || keys[0] != "u/a.jpg" {
		t.Errorf("keys = %v, want only u/a.jpg", keys)
	}
}

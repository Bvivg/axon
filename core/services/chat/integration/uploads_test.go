//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
	"github.com/bvivg/axon/core/services/chat/internal/uploads"
)

const testPartBytes = 5 << 20

func noisyPNG(t *testing.T, seed uint64) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed))
	img := image.NewNRGBA(image.Rect(0, 0, 1500, 1500))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32()), 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	if buf.Len() <= testPartBytes {
		t.Fatalf("the test image is %d bytes, want more than one part", buf.Len())
	}
	return buf.Bytes()
}

func newUploads(t *testing.T) (*uploads.Manager, context.CancelFunc) {
	t.Helper()
	repo := newRepo(t)
	sweeper, err := attachment.NewSweeper(attachment.SweeperConfig{
		Objects:     attachmentStore,
		Uploads:     repo,
		URLs:        attachmentURLs,
		Interval:    time.Hour,
		OrphanAfter: time.Hour,
		Logger:      slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}
	m, err := uploads.New(uploads.Config{
		Sessions:      repo,
		Objects:       attachmentStore,
		Processor:     attachment.NewPipeline(attachmentStore, attachmentURLs, media.NewTools(media.ToolsConfig{})),
		KeysOf:        sweeper.ObjectKeys,
		Logger:        slog.New(slog.DiscardHandler),
		MaxBytes:      50 << 20,
		MaxVoiceBytes: 10 << 20,
		PartSize:      testPartBytes,
		TempDir:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("uploads.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go m.Run(ctx)
	return m, cancel
}

func sendInParts(t *testing.T, m *uploads.Manager, user uuid.UUID, raw []byte, order ...int) domain.UploadSession {
	t.Helper()
	s, err := m.Start(t.Context(), user, domain.UploadModeMedia, "noise.png", int64(len(raw)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, n := range order {
		start := int64(n-1) * s.PartSize
		part := raw[start : start+s.PartBytes(n)]
		if err := m.PutPart(t.Context(), user, s.ID, n, bytes.NewReader(part), int64(len(part))); err != nil {
			t.Fatalf("PutPart(%d): %v", n, err)
		}
	}
	if _, err := m.Complete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	return s
}

func awaitReady(t *testing.T, m *uploads.Manager, user, id uuid.UUID) uploads.View {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		view, err := m.Wait(t.Context(), user, id, 10*time.Second)
		if err != nil {
			t.Fatalf("Wait: %v", err)
		}
		switch view.Session.Status {
		case domain.UploadReady:
			return view
		case domain.UploadFailed:
			t.Fatalf("processing failed: %s", view.Session.Failure)
		}
	}
	t.Fatal("the upload never became ready")
	return uploads.View{}
}

func publicURLOf(t *testing.T, payload json.RawMessage) string {
	t.Helper()
	var fields struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(payload, &fields); err != nil || fields.URL == "" {
		t.Fatalf("payload %s has no url", payload)
	}
	return fields.URL
}

func reachable(t *testing.T, url string) bool {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestAnUploadInPartsIsProcessedOnceAndSweptWhenNobodyUsesIt(t *testing.T) {
	m, stop := newUploads(t)
	defer stop()
	repo := newRepo(t)

	raw := noisyPNG(t, 1)
	alice, bob := uuid.New(), uuid.New()

	first := sendInParts(t, m, alice, raw, 2, 1)
	ready := awaitReady(t, m, alice, first.ID)
	if ready.Upload == nil || ready.Upload.FileID == nil || ready.Upload.Kind != domain.MessageKindImage {
		t.Fatalf("view = %+v, want an image upload backed by a stored file", ready)
	}
	url := publicURLOf(t, ready.Upload.Payload)
	if !reachable(t, url) {
		t.Fatalf("the processed image is not served at %s", url)
	}
	if err := attachmentStore.Download(t.Context(), first.StagingKey, filepath.Join(t.TempDir(), "staged")); err == nil {
		t.Error("the staged original outlived processing")
	}

	second := sendInParts(t, m, bob, raw, 1, 2)
	again := awaitReady(t, m, bob, second.ID)
	if *again.Upload.FileID != *ready.Upload.FileID || publicURLOf(t, again.Upload.Payload) != url {
		t.Fatalf("the same bytes became another file: %v vs %v", again.Upload.FileID, ready.Upload.FileID)
	}

	if err := m.Delete(t.Context(), alice, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := m.Delete(t.Context(), bob, second.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	swept, err := repo.SweepOrphanFiles(t.Context(), time.Now().Add(time.Hour), 10,
		func(ctx context.Context, files []domain.StoredFile) ([]uuid.UUID, error) {
			var ids []uuid.UUID
			for _, f := range files {
				if failures := attachmentStore.Remove(ctx, f.ObjectKeys); len(failures) == 0 {
					ids = append(ids, f.ID)
				}
			}
			return ids, nil
		})
	if err != nil {
		t.Fatalf("SweepOrphanFiles: %v", err)
	}
	if swept < 1 {
		t.Fatalf("swept %d files, want the unused one", swept)
	}
	if reachable(t, url) {
		t.Error("the swept file is still served")
	}
}

func TestCompleteRefusesAnUploadWithAPartMissing(t *testing.T) {
	m, stop := newUploads(t)
	defer stop()

	raw := noisyPNG(t, 2)
	user := uuid.New()
	s, err := m.Start(t.Context(), user, domain.UploadModeMedia, "noise.png", int64(len(raw)))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.PutPart(t.Context(), user, s.ID, 2, bytes.NewReader(raw[s.PartSize:]), s.PartBytes(2)); err != nil {
		t.Fatalf("PutPart: %v", err)
	}

	view, err := m.Status(t.Context(), user, s.ID)
	if err != nil || len(view.Parts) != 1 || view.Parts[0] != 2 {
		t.Fatalf("status = %+v, %v; want part 2 listed as received", view, err)
	}
	if _, err := m.Complete(t.Context(), user, s.ID); err == nil {
		t.Fatal("an upload with part 1 missing was completed")
	}
	if err := m.Delete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

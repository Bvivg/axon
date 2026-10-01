package attachment_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

var urls = attachment.NewURLBuilder("http://files.test", "chat-attachments")

type objects struct {
	calls   int
	refused map[string]bool
}

func (o *objects) Remove(_ context.Context, keys []string) map[string]error {
	o.calls++
	failed := map[string]error{}
	for _, key := range keys {
		if o.refused[key] {
			failed[key] = errors.New("access denied")
		}
	}
	return failed
}

type orphans struct {
	batches [][]domain.Upload
	swept   []uuid.UUID
}

func (o *orphans) SweepOrphanUploads(
	ctx context.Context,
	_ time.Time,
	_ int,
	remove func(context.Context, []domain.Upload) ([]uuid.UUID, error),
) (int, error) {
	if len(o.batches) == 0 {
		return 0, nil
	}
	batch := o.batches[0]
	o.batches = o.batches[1:]

	ids, err := remove(ctx, batch)
	if err != nil {
		return 0, err
	}
	o.swept = append(o.swept, ids...)
	return len(ids), nil
}

func upload(t *testing.T, keys ...string) domain.Upload {
	t.Helper()

	payload := map[string]string{}
	for i, key := range keys {
		payload[[]string{"url", "thumbnail_url"}[i]] = urls.URL(key)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return domain.Upload{ID: uuid.New(), Kind: domain.MessageKindImage, Payload: raw}
}

func newSweeper(t *testing.T, o *objects, u *orphans) *attachment.Sweeper {
	t.Helper()

	sweeper, err := attachment.NewSweeper(attachment.SweeperConfig{
		Objects:     o,
		Uploads:     u,
		URLs:        urls,
		Interval:    time.Hour,
		OrphanAfter: time.Hour,
		Logger:      logger.Discard(),
	})
	if err != nil {
		t.Fatalf("NewSweeper: %v", err)
	}
	return sweeper
}

func TestOneStuckFileDoesNotHoldTheOthersBack(t *testing.T) {
	stuck := upload(t, "u/stuck.jpg", "u/stuck.thumb.jpg")
	fine := upload(t, "u/fine.jpg", "u/fine.thumb.jpg")
	alsoFine := upload(t, "u/also.jpg")

	o := &objects{refused: map[string]bool{"u/stuck.thumb.jpg": true}}
	u := &orphans{batches: [][]domain.Upload{{stuck, fine, alsoFine}}}

	swept, err := newSweeper(t, o, u).Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 2 {
		t.Errorf("swept %d, want 2", swept)
	}
	if !slices.Equal(u.swept, []uuid.UUID{fine.ID, alsoFine.ID}) {
		t.Errorf("swept uploads = %v, want the two whose files went away", u.swept)
	}
	if o.calls != 1 {
		t.Errorf("object store called %d times, want one batched call", o.calls)
	}
}

func TestNothingIsForgottenWhenTheStoreRefusesEverything(t *testing.T) {
	only := upload(t, "u/only.jpg")

	o := &objects{refused: map[string]bool{"u/only.jpg": true}}
	u := &orphans{batches: [][]domain.Upload{{only}}}

	if _, err := newSweeper(t, o, u).Sweep(t.Context()); err == nil {
		t.Fatal("Sweep succeeded although no file could be removed")
	}
	if len(u.swept) != 0 {
		t.Errorf("swept = %v, want the upload kept for the next pass", u.swept)
	}
}

func TestAStuckFileDoesNotEndThePassEarly(t *testing.T) {
	stuck := upload(t, "u/stuck.jpg")
	first := upload(t, "u/first.jpg")
	later := upload(t, "u/later.jpg")

	o := &objects{refused: map[string]bool{"u/stuck.jpg": true}}
	u := &orphans{batches: [][]domain.Upload{{stuck, first}, {stuck, later}}}

	swept, err := newSweeper(t, o, u).Sweep(t.Context())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 2 || !slices.Equal(u.swept, []uuid.UUID{first.ID, later.ID}) {
		t.Errorf("swept %d (%v), want both removable uploads in one pass", swept, u.swept)
	}
}

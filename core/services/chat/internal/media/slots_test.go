package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/chat/internal/media"
)

func acquire(t *testing.T, s *media.Slots, owner string) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	release, err := s.Acquire(ctx, owner)
	if err != nil {
		t.Fatalf("Acquire(%s): %v", owner, err)
	}
	return release
}

func blocked(t *testing.T, s *media.Slots, owner string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if release, err := s.Acquire(ctx, owner); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("Acquire(%s) = %v, want it to wait for a slot", owner, err)
	}
}

func TestOneUserCannotTakeEverySlot(t *testing.T) {
	s := media.NewSlots(2, 1)

	mallory := acquire(t, s, "mallory")
	blocked(t, s, "mallory")

	alice := acquire(t, s, "alice")
	blocked(t, s, "bob")

	mallory()
	acquire(t, s, "bob")()
	alice()
}

func TestWaitingUsersTakeTurns(t *testing.T) {
	s := media.NewSlots(1, 1)
	first := acquire(t, s, "mallory")

	var (
		mu    sync.Mutex
		order []string
		wg    sync.WaitGroup
	)
	enqueue := func(owner string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := s.Acquire(t.Context(), owner)
			if err != nil {
				t.Errorf("Acquire(%s): %v", owner, err)
				return
			}
			mu.Lock()
			order = append(order, owner)
			mu.Unlock()
			release()
		}()
		time.Sleep(25 * time.Millisecond)
	}
	for range 3 {
		enqueue("mallory")
	}
	enqueue("alice")
	enqueue("bob")

	first()
	wg.Wait()

	want := []string{"mallory", "alice", "bob", "mallory", "mallory"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestAGivenUpWaitLeavesNoSlotBehind(t *testing.T) {
	s := media.NewSlots(1, 1)
	held := acquire(t, s, "alice")
	blocked(t, s, "bob")
	blocked(t, s, "alice")
	held()

	acquire(t, s, "bob")()
	acquire(t, s, "alice")()
}

func TestImageProcessingWaitsForAFreeSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, encodeJPEG(t, solid(16, 16, color.White, color.Black)), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	slots := media.NewSlots(1, 1)
	images := media.NewImages(media.ImageLimits{}, slots)

	held := acquire(t, slots, "alice")
	ctx, cancel := context.WithTimeout(media.WithOwner(t.Context(), "bob"), 50*time.Millisecond)
	defer cancel()
	if _, err := images.ProcessFile(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ProcessFile with every slot taken = %v, want it to wait", err)
	}
	held()

	got, err := images.ProcessFile(media.WithOwner(t.Context(), "bob"), path)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(got.Data)); err != nil || cfg.Width != 16 {
		t.Fatalf("processed image = %+v, %v", cfg, err)
	}
}

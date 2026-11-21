package uploads

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func TestOneUploaderCannotHoldUpEveryoneElse(t *testing.T) {
	objects := newFakeObjects()
	sessions := newFakeSessions()
	mallory, alice := uuid.New(), uuid.New()
	unblock := make(chan struct{})
	processor := &fakeProcessor{objects: objects, hold: func(user uuid.UUID) {
		if user == mallory {
			<-unblock
		}
	}}
	m, err := New(Config{
		Sessions:      sessions,
		Objects:       objects,
		Processor:     processor,
		Budget:        NewBudget(newFakeUsage(), 0),
		KeysOf:        keysOf,
		Logger:        slog.New(slog.DiscardHandler),
		MaxBytes:      4 * testPart,
		MaxVoiceBytes: testPart,
		PartSize:      testPart,
		Workers:       2,
		PerUploader:   1,
		TempDir:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h := harness{m: m, sessions: sessions, objects: objects, processor: processor}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go m.Run(ctx)
	defer close(unblock)

	for i := range 6 {
		s := h.upload(t, mallory, content(100, byte(30+i)))
		if _, err := m.Complete(t.Context(), mallory, s.ID); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	mine := h.upload(t, alice, content(100, 40))
	if _, err := m.Complete(t.Context(), alice, mine.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	view, err := m.Wait(t.Context(), alice, mine.ID, 5*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if view.Session.Status != domain.UploadReady {
		t.Fatalf("alice's upload is %s while mallory's queue is stuck, want ready", view.Session.Status)
	}
}

package pubsub

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestCallOverflowClosesInsteadOfSilentlyDropping(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewMemory()
	user := uuid.New()
	ch, stop, err := bus.SubscribeUserSignals(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i := 0; i < Backlog+1; i++ {
		if err := bus.PublishUserSignal(ctx, user, Signal{Kind: SignalCallState, UserID: user, EventID: uuid.New()}); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for range ch {
		count++
	}
	if count != Backlog {
		t.Fatalf("queued %d", count)
	}
}
func TestPublishAndCloseAreSafeTogether(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := NewMemory()
	user := uuid.New()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, stop, err := bus.SubscribeUserSignals(ctx, user)
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 100; j++ {
				_ = bus.PublishUserSignal(ctx, user, Signal{Kind: SignalCallState, UserID: user, EventID: uuid.New()})
			}
			stop()
			stop()
		}()
	}
	wg.Wait()
}

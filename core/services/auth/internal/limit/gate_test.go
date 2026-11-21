package limit_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/auth/internal/limit"
)

func TestAFullGateFailsFastWithoutAWait(t *testing.T) {
	gate := limit.NewGate(1, 0)

	release, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	start := time.Now()
	if _, err := gate.Acquire(t.Context()); !errors.Is(err, limit.ErrSaturated) {
		t.Fatalf("err = %v, want ErrSaturated", err)
	}
	if waited := time.Since(start); waited > 50*time.Millisecond {
		t.Errorf("a gate without a wait blocked for %v", waited)
	}

	release()
	again, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	again()
}

func TestAFullGateGivesUpAfterItsWait(t *testing.T) {
	gate := limit.NewGate(1, 30*time.Millisecond)

	release, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	start := time.Now()
	if _, err := gate.Acquire(t.Context()); !errors.Is(err, limit.ErrSaturated) {
		t.Fatalf("err = %v, want ErrSaturated", err)
	}
	if waited := time.Since(start); waited < 25*time.Millisecond {
		t.Errorf("gave up after %v, before the wait elapsed", waited)
	}
}

func TestAWaiterGetsASlotThatFreesInTime(t *testing.T) {
	gate := limit.NewGate(1, time.Second)

	release, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	time.AfterFunc(20*time.Millisecond, release)

	next, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("the waiter did not get the freed slot: %v", err)
	}
	next()
}

func TestAWaiterStopsWhenItsRequestIsCancelled(t *testing.T) {
	gate := limit.NewGate(1, time.Minute)

	release, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the request's deadline", err)
	}
}

func TestTheGateNeverAdmitsMoreThanItsSize(t *testing.T) {
	const size = 3
	gate := limit.NewGate(size, time.Second)

	var inside, peak atomic.Int32
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			release, err := gate.Acquire(t.Context())
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			defer release()
			n := inside.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inside.Add(-1)
		})
	}
	wg.Wait()

	if got := peak.Load(); got > size {
		t.Fatalf("%d holders at once, want at most %d", got, size)
	}
}

func TestAGateHasAtLeastOneSlot(t *testing.T) {
	gate := limit.NewGate(0, 0)

	release, err := gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
}

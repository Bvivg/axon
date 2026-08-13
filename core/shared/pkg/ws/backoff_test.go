package ws

import (
	"errors"
	"testing"
	"time"
)

func TestDelayGrowsByTheFactor(t *testing.T) {
	b := Backoff{Initial: 100 * time.Millisecond, Max: time.Minute, Factor: 2}

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}
	for attempt, expected := range want {
		if got := b.delay(attempt, 0); got != expected {
			t.Errorf("delay(%d) = %v, want %v", attempt, got, expected)
		}
	}
}

func TestDelayIsCappedAtMax(t *testing.T) {
	b := Backoff{Initial: 100 * time.Millisecond, Max: time.Second, Factor: 2}

	if got := b.delay(20, 0); got != time.Second {
		t.Fatalf("delay(20) = %v, want the 1s cap", got)
	}
}

func TestDelaySurvivesOverflow(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: 30 * time.Second, Factor: 2}

	if got := b.delay(10_000, 0); got != 30*time.Second {
		t.Fatalf("delay(10000) = %v, want the 30s cap", got)
	}
}

func TestDelayTreatsNegativeAttemptsAsTheFirst(t *testing.T) {
	b := Backoff{Initial: 100 * time.Millisecond, Max: time.Minute, Factor: 2}

	if got := b.delay(-3, 0); got != 100*time.Millisecond {
		t.Fatalf("delay(-3) = %v, want the initial delay", got)
	}
}

func TestJitterOnlySubtracts(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: time.Minute, Factor: 2, Jitter: 0.5}

	if got := b.delay(0, 0); got != time.Second {
		t.Fatalf("delay with random 0 = %v, want the full second", got)
	}
	if got := b.delay(0, 1); got != 500*time.Millisecond {
		t.Fatalf("delay with random 1 = %v, want half a second", got)
	}
}

func TestDelayStaysWithinItsJitterWindow(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: time.Minute, Factor: 2, Jitter: 0.2}

	seen := make(map[time.Duration]struct{})
	for range 200 {
		got := b.Delay(0)
		if got > time.Second || got < 800*time.Millisecond {
			t.Fatalf("Delay(0) = %v, want it within [800ms, 1s]", got)
		}
		seen[got] = struct{}{}
	}

	if len(seen) < 2 {
		t.Fatal("Delay returned the same value 200 times, so jitter is not applied")
	}
}

func TestBackoffValidate(t *testing.T) {
	valid := map[string]Backoff{
		"default":   DefaultBackoff,
		"no jitter": {Initial: time.Second, Max: time.Second, Factor: 1},
	}
	for name, b := range valid {
		t.Run(name, func(t *testing.T) {
			if err := b.Validate(); err != nil {
				t.Fatalf("Validate = %v, want nil", err)
			}
		})
	}

	invalid := map[string]Backoff{
		"zero initial":      {Max: time.Second, Factor: 2},
		"negative initial":  {Initial: -time.Second, Max: time.Second, Factor: 2},
		"max below initial": {Initial: 10 * time.Second, Max: time.Second, Factor: 2},
		"shrinking factor":  {Initial: time.Second, Max: time.Minute, Factor: 0.5},
		"negative jitter":   {Initial: time.Second, Max: time.Minute, Factor: 2, Jitter: -0.1},
		"jitter above one":  {Initial: time.Second, Max: time.Minute, Factor: 2, Jitter: 1.5},
	}
	for name, b := range invalid {
		t.Run(name, func(t *testing.T) {
			if err := b.Validate(); !errors.Is(err, ErrInvalidBackoff) {
				t.Fatalf("Validate = %v, want ErrInvalidBackoff", err)
			}
		})
	}
}

package matchmaking

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWait(t *testing.T) {
	tests := []struct {
		active int
		want   time.Duration
	}{
		{-3, MinWait},
		{0, MinWait},
		{2, MinWait},
		{20, 10 * time.Second},
		{80, 20 * time.Second},
		{200, MaxWait},
		{10000, MaxWait},
	}
	for _, tt := range tests {
		if got := Wait(tt.active); got != tt.want {
			t.Errorf("Wait(%d) = %v, want %v", tt.active, got, tt.want)
		}
	}
}

func TestWindow(t *testing.T) {
	wait := 10 * time.Second
	tests := []struct {
		waited time.Duration
		want   int
	}{
		{-time.Second, InitialWindow},
		{0, InitialWindow},
		{5 * time.Second, 250},
		{9 * time.Second, 370},
		{10 * time.Second, Unlimited},
		{time.Minute, Unlimited},
	}
	for _, tt := range tests {
		if got := Window(tt.waited, wait); got != tt.want {
			t.Errorf("Window(%v) = %d, want %d", tt.waited, got, tt.want)
		}
	}
}

func entry(rating int, waited time.Duration, now time.Time) Entry {
	return Entry{User: uuid.New(), Rating: rating, JoinedAt: now.Add(-waited)}
}

func TestMatch(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	wait := 10 * time.Second

	t.Run("nobody else in the queue", func(t *testing.T) {
		if pairs := Match([]Entry{entry(1200, time.Minute, now)}, now, wait); len(pairs) != 0 {
			t.Fatalf("pairs = %v", pairs)
		}
	})

	t.Run("closest rating wins over the longest wait", func(t *testing.T) {
		first := entry(1200, 3*time.Second, now)
		far := entry(1390, 2*time.Second, now)
		near := entry(1230, time.Second, now)
		pairs := Match([]Entry{near, far, first}, now, wait)
		if len(pairs) != 1 || pairs[0].First.User != first.User || pairs[0].Second.User != near.User {
			t.Fatalf("pairs = %+v", pairs)
		}
	})

	t.Run("far ratings wait while both are fresh", func(t *testing.T) {
		pairs := Match([]Entry{entry(1000, time.Second, now), entry(1600, time.Second, now)}, now, wait)
		if len(pairs) != 0 {
			t.Fatalf("pairs = %+v", pairs)
		}
	})

	t.Run("after the wait anyone is accepted", func(t *testing.T) {
		pairs := Match([]Entry{entry(1000, 11*time.Second, now), entry(1900, 0, now)}, now, wait)
		if len(pairs) != 1 {
			t.Fatalf("pairs = %+v", pairs)
		}
	})

	t.Run("window widens with waiting time", func(t *testing.T) {
		pairs := Match([]Entry{entry(1000, 6*time.Second, now), entry(1250, time.Second, now)}, now, wait)
		if len(pairs) != 1 {
			t.Fatalf("pairs = %+v", pairs)
		}
	})

	t.Run("each player is matched once", func(t *testing.T) {
		entries := []Entry{entry(1200, 4*time.Second, now), entry(1210, 3*time.Second, now), entry(1220, 2*time.Second, now), entry(1230, time.Second, now), entry(1240, 0, now)}
		pairs := Match(entries, now, wait)
		seen := map[uuid.UUID]bool{}
		for _, pair := range pairs {
			for _, user := range []uuid.UUID{pair.First.User, pair.Second.User} {
				if seen[user] {
					t.Fatalf("%s matched twice", user)
				}
				seen[user] = true
			}
		}
		if len(pairs) != 2 {
			t.Fatalf("pairs = %d, want 2", len(pairs))
		}
	})
}

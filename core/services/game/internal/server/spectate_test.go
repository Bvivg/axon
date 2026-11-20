package server

import (
	"testing"

	"github.com/google/uuid"
)

func TestWatchersCountPerGameAndRespectTheLimit(t *testing.T) {
	w := newWatchers(2)
	first, second := uuid.New(), uuid.New()
	if !w.join(first) || !w.join(first) {
		t.Fatal("two spectators must fit under a limit of two")
	}
	if w.join(first) {
		t.Fatal("a third spectator joined a full game")
	}
	if !w.join(second) {
		t.Fatal("the limit is per game")
	}
	if w.count(first) != 2 || w.count(second) != 1 || w.total() != 3 {
		t.Fatalf("counts = %d/%d, total %d", w.count(first), w.count(second), w.total())
	}
	w.leave(first)
	w.leave(second)
	w.leave(second)
	if w.count(first) != 1 || w.count(second) != 0 || w.total() != 1 {
		t.Fatalf("after leaving: counts = %d/%d, total %d", w.count(first), w.count(second), w.total())
	}
	if !w.join(first) {
		t.Fatal("a freed seat must be reusable")
	}
}

func TestWatchersDefaultLimit(t *testing.T) {
	if newWatchers(0).limit != defaultWatchers {
		t.Fatal("a zero limit must fall back to the default")
	}
}

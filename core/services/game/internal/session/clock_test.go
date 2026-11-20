package session

import (
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
)

func TestClock(t *testing.T) {
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	for _, turn := range []int{0, 1} {
		state := tictactoe.New().Init([]engine.PlayerID{"x", "o"})
		state.Turn = turn
		game := Session{State: state, Clock: NewClock(start)}
		clock := game.ClockAt(start.Add(1234 * time.Millisecond))
		if clock.RemainingMS[turn] != TimeLimitMS-1234 || clock.RemainingMS[1-turn] != TimeLimitMS {
			t.Fatal(clock)
		}
		if game.Clock.RemainingMS[turn] != TimeLimitMS {
			t.Fatal("snapshot mutated")
		}
		if game.ClockAt(start.Add(-time.Second)).RemainingMS[turn] != TimeLimitMS {
			t.Fatal("negative elapsed")
		}
		if game.expire(start.Add(5*time.Minute - time.Millisecond)) {
			t.Fatal("expired early")
		}
		if !game.expire(start.Add(5*time.Minute)) || !game.TimedOut || game.State.Winner != 1-turn {
			t.Fatal(game)
		}
		if game.Clock.RemainingMS[turn] != 0 {
			t.Fatal(game.Clock)
		}
		if game.expire(start.Add(6 * time.Minute)) {
			t.Fatal("expired twice")
		}
		if game.ClockAt(start.Add(6*time.Minute)).RemainingMS != game.Clock.RemainingMS {
			t.Fatal("completed clock keeps running")
		}
	}
}

func TestWaitingClock(t *testing.T) {
	game := Session{}
	if game.ClockAt(time.Now()) != nil || game.expire(time.Now()) {
		t.Fatal("waiting game has no active clock")
	}
}

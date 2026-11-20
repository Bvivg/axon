package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func TestStartTournamentCreatesATaggedRatedGame(t *testing.T) {
	r := rules(t)
	id, tournament, creator, opponent := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	game, change, err := r.StartTournament(id, tictactoe.Key, tournament, creator, opponent, now)
	if err != nil {
		t.Fatal(err)
	}
	if game.ID != id || game.Tournament == nil || *game.Tournament != tournament || !game.Rated || game.Opponent == nil || *game.Opponent != opponent {
		t.Fatalf("game = %+v", game)
	}
	if change.Fact != session.FactCreated || game.Clock == nil {
		t.Fatalf("change = %+v, clock = %v", change, game.Clock)
	}
}

func TestForfeitResignsThePlayerWhoNeverMoved(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	now := time.Now()
	play := func(game session.Session, moves ...[2]int) session.Session {
		t.Helper()
		for ply, move := range moves {
			player := creator
			if ply%2 == 1 {
				player = opponent
			}
			next, _, err := r.Move(game, player, ply, tictactoe.Key, cell(t, move[0], move[1]), nil, now)
			if err != nil {
				t.Fatal(err)
			}
			game = next
		}
		return game
	}
	fresh := started(t, r, creator, opponent)
	expired := started(t, r, creator, opponent)
	expired.Clock = &session.Clock{RemainingMS: [2]int64{50, 50}, StartedAt: now.Add(-time.Second)}
	over := play(fresh, [2]int{0, 0}, [2]int{1, 0}, [2]int{0, 1}, [2]int{1, 1}, [2]int{0, 2})
	cases := []struct {
		name     string
		game     session.Session
		fact     session.Fact
		winner   int
		absent   uuid.UUID
		timedOut bool
	}{
		{name: "nobody moved, the first player is absent", game: fresh, fact: session.FactResigned, winner: 1, absent: creator},
		{name: "only the first player moved", game: play(fresh, [2]int{0, 0}), fact: session.FactResigned, winner: 0, absent: opponent},
		{name: "both players moved", game: play(fresh, [2]int{0, 0}, [2]int{1, 1}), fact: session.FactNone},
		{name: "the game is over", game: over, fact: session.FactNone},
		{name: "the clock ran out first", game: expired, fact: session.FactTimedOut, winner: 1, timedOut: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next, change, err := r.Forfeit(c.game, now)
			if err != nil {
				t.Fatal(err)
			}
			if change.Fact != c.fact {
				t.Fatalf("fact = %v, want %v", change.Fact, c.fact)
			}
			if c.fact == session.FactNone {
				if next.State.Ply != c.game.State.Ply || next.State.Status != c.game.State.Status {
					t.Fatalf("a no-op forfeit changed the game: %+v", next.State)
				}
				return
			}
			if next.State.Status != engine.StatusWin || next.State.Winner != c.winner || next.TimedOut != c.timedOut {
				t.Fatalf("state = %+v, timed out %v", next.State, next.TimedOut)
			}
			if c.fact == session.FactResigned && (!next.Resigned || change.Actor != c.absent) {
				t.Fatalf("resigned %v by %s, want %s", next.Resigned, change.Actor, c.absent)
			}
		})
	}
	waiting := session.Session{ID: uuid.New(), Game: tictactoe.Key, Creator: creator}
	if _, _, err := r.Forfeit(waiting, now); !errors.Is(err, session.ErrNotPlaying) {
		t.Fatalf("forfeit of a game without an opponent = %v", err)
	}
}

func TestAbortCancelsOnlyUnfinishedGames(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	now := time.Now()
	playing := started(t, r, creator, opponent)
	if _, change := r.Abort(playing, now); change.Fact != session.FactCancelled || change.At != now {
		t.Fatalf("abort of a live game = %+v", change)
	}
	finished := playing
	finished.State = finished.State.Finish(0)
	if _, change := r.Abort(finished, now); change.Recorded() {
		t.Fatalf("abort of a finished game recorded %+v", change)
	}
}

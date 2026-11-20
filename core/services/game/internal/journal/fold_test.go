package journal_test

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func registry(t *testing.T) *engine.Registry {
	t.Helper()
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition(), quoridor.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return games
}

func roundTrip(t *testing.T, game session.Session, change session.Change) journal.Event {
	t.Helper()
	raw, err := journal.Encode(journal.Entry{Event: &journal.Event{Session: game, Change: change, Generation: 1}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := journal.Decode(0, 0, raw)
	if err != nil {
		t.Fatal(err)
	}
	return *entry.Event
}

func same(t *testing.T, want, got session.Session) {
	t.Helper()
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Fatalf("replayed session differs\nwant %s\ngot  %s", a, b)
	}
}

func TestFoldReplaysRandomGamesExactly(t *testing.T) {
	games := registry(t)
	rules := session.NewRules(games)
	for _, key := range []engine.Key{tictactoe.Key, chess.Key, quoridor.Key} {
		for seed := range uint64(12) {
			rng := rand.New(rand.NewPCG(seed, uint64(len(key))))
			creator, opponent := uuid.New(), uuid.New()
			now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
			game, change, err := rules.Start(uuid.New(), key, creator, opponent, false, now)
			if err != nil {
				t.Fatal(err)
			}
			game = session.Record(game, change)
			folded, err := journal.Fold(rules, nil, roundTrip(t, game, change))
			if err != nil {
				t.Fatal(err)
			}
			replica := folded.Session
			def, _ := games.Get(key)
			for ply := 0; ply < 400 && !game.State.Over(); ply++ {
				player := game.State.Players[game.State.Turn]
				moves := def.ValidMoves(game.State, player)
				if len(moves) == 0 {
					break
				}
				move := moves[rng.IntN(len(moves))]
				user := creator
				if string(player) == opponent.String() {
					user = opponent
				}
				now = now.Add(time.Duration(rng.IntN(1500)+1) * time.Millisecond).Add(time.Duration(rng.IntN(1000)) * time.Microsecond)
				next, change, err := rules.Move(game, user, game.State.Ply, key, move.Data, nil, now)
				if err != nil {
					t.Fatalf("%s seed %d ply %d: %v", key, seed, ply, err)
				}
				game = session.Record(next, change)
				event := roundTrip(t, game, change)
				if change.Fact == session.FactMoved && !event.Partial {
					t.Fatalf("move event carries a snapshot")
				}
				folded, err := journal.Fold(rules, &replica, event)
				if err != nil {
					t.Fatalf("%s seed %d ply %d: %v", key, seed, ply, err)
				}
				if !folded.Applied {
					t.Fatalf("%s seed %d ply %d: event skipped", key, seed, ply)
				}
				same(t, game, folded.Session)
				replica = folded.Session
			}
		}
	}
}

func TestFoldSkipsKnownRevisionsAndRejectsGaps(t *testing.T) {
	games := registry(t)
	rules := session.NewRules(games)
	creator, opponent := uuid.New(), uuid.New()
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	game, change, _ := rules.Start(uuid.New(), tictactoe.Key, creator, opponent, false, now)
	game = session.Record(game, change)
	base := game
	data, _ := json.Marshal(tictactoe.Cell{Row: 1, Col: 1})
	first, change, err := rules.Move(game, creator, 0, tictactoe.Key, data, nil, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	first = session.Record(first, change)
	firstEvent := roundTrip(t, first, change)
	data, _ = json.Marshal(tictactoe.Cell{Row: 0, Col: 0})
	second, change, err := rules.Move(first, opponent, 1, tictactoe.Key, data, nil, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	second = session.Record(second, change)
	secondEvent := roundTrip(t, second, change)

	if folded, err := journal.Fold(rules, &second, firstEvent); err != nil || folded.Applied {
		t.Fatalf("known revision must be skipped, applied=%v err=%v", folded.Applied, err)
	}
	if _, err := journal.Fold(rules, &base, secondEvent); !errors.Is(err, journal.ErrGap) {
		t.Fatalf("gap = %v, want ErrGap", err)
	}
	if _, err := journal.Fold(rules, nil, secondEvent); !errors.Is(err, journal.ErrGap) {
		t.Fatalf("missing base = %v, want ErrGap", err)
	}
	tampered := secondEvent
	outcome := *tampered.Outcome
	outcome.Turn = 1 - outcome.Turn
	tampered.Outcome = &outcome
	if _, err := journal.Fold(rules, &first, tampered); !errors.Is(err, journal.ErrDiverged) {
		t.Fatalf("diverged = %v, want ErrDiverged", err)
	}
}

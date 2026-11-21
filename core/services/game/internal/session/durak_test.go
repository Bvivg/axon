package session_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func cardRules(t *testing.T) session.Rules {
	t.Helper()
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), durak.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return session.NewRules(games)
}

func TestEveryDealDrawsAFreshSecretSeed(t *testing.T) {
	r := cardRules(t)
	creator, opponent := uuid.New(), uuid.New()
	now := time.Now()
	seen := map[uint64]bool{}
	for range 8 {
		game, _, err := r.Start(uuid.New(), durak.Key, []uuid.UUID{creator, opponent}, session.Table{}, false, now)
		if err != nil {
			t.Fatal(err)
		}
		if game.State.Seed == 0 || seen[game.State.Seed] {
			t.Fatalf("seed %d repeated or empty", game.State.Seed)
		}
		seen[game.State.Seed] = true
	}
	waiting, _, err := r.Create(creator, uuid.New(), durak.Key, session.Table{}, now)
	if err != nil {
		t.Fatal(err)
	}
	joined, change, err := r.Join(waiting, opponent, now)
	if err != nil || change.Fact != session.FactJoined || joined.State.Seed == 0 || seen[joined.State.Seed] {
		t.Fatalf("join dealt %+v, %v", joined.State, err)
	}
	ticTacToe, _, err := r.Start(uuid.New(), tictactoe.Key, []uuid.UUID{creator, opponent}, session.Table{}, false, now)
	if err != nil || ticTacToe.State.Seed != 0 {
		t.Fatalf("an open-information game kept a seed: %d, %v", ticTacToe.State.Seed, err)
	}
}

func TestDurakHistoryReplaysFromTheRecordedSeed(t *testing.T) {
	r := cardRules(t).WithSeeds(func() uint64 { return 7 })
	creator, opponent := uuid.New(), uuid.New()
	now := time.Now()
	game, _, err := r.Start(uuid.New(), durak.Key, []uuid.UUID{creator, opponent}, session.Table{}, false, now)
	if err != nil {
		t.Fatal(err)
	}
	users := map[engine.PlayerID]uuid.UUID{engine.PlayerID(creator.String()): creator, engine.PlayerID(opponent.String()): opponent}
	var moves []session.MoveRecord
	for range 12 {
		player := game.State.Players[game.State.Turn]
		legal := durak.New().ValidMoves(game.State, player)
		next, change, err := r.Move(game, users[player], game.State.Ply, durak.Key, legal[0].Data, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		moves = append(moves, session.MoveRecord{Ply: change.Move.Ply, Player: users[player], Data: change.Move.Data})
		game = next
	}
	reseeded := cardRules(t)
	var frames []engine.State
	collect := func(state engine.State) error {
		frames = append(frames, state)
		return nil
	}
	if err := reseeded.Frames(t.Context(), game, moves, collect); err != nil {
		t.Fatal(err)
	}
	if last := frames[len(frames)-1]; string(last.Data) != string(game.State.Data) || last.Turn != game.State.Turn || last.Seed != 7 {
		t.Fatal("frames diverged from the live game")
	}
	replayed, err := reseeded.Replay(game, moves[:5])
	if err != nil || string(replayed.Data) != string(frames[5].Data) {
		t.Fatal("replay diverged", err)
	}
	wrong := game
	wrong.State.Seed = 8
	if err := reseeded.Frames(t.Context(), wrong, moves, func(engine.State) error { return nil }); err == nil {
		t.Fatal("a different seed replayed the same history")
	}
}

func TestDurakRefusesUndo(t *testing.T) {
	r := cardRules(t)
	creator, opponent := uuid.New(), uuid.New()
	game, _, err := r.Start(uuid.New(), durak.Key, []uuid.UUID{creator, opponent}, session.Table{}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	player := game.State.Players[game.State.Turn]
	user := creator
	if string(player) == opponent.String() {
		user = opponent
	}
	game, _, err = r.Move(game, user, 0, durak.Key, durak.New().ValidMoves(game.State, player)[0].Data, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	previous := func(session.Session) (engine.State, *session.Clock, error) {
		t.Fatal("undo looked up the previous position")
		return engine.State{}, nil, nil
	}
	if _, _, err := r.Undo(game, user, nil, previous, time.Now()); !errors.Is(err, session.ErrNoUndo) {
		t.Fatalf("undo = %v, want ErrNoUndo", err)
	}
}

package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func frames(ctx context.Context, r session.Rules, game session.Session, moves []session.MoveRecord) ([]engine.State, error) {
	var out []engine.State
	err := r.Frames(ctx, game, moves, func(state engine.State) error {
		out = append(out, state)
		return nil
	})
	return out, err
}

func rules(t *testing.T) session.Rules {
	t.Helper()
	games := engine.NewRegistry()
	if err := games.Register(tictactoe.Definition()); err != nil {
		t.Fatal(err)
	}
	return session.NewRules(games)
}

func started(t *testing.T, r session.Rules, creator, opponent uuid.UUID) session.Session {
	t.Helper()
	state, err := r.Games().NewState(tictactoe.Key, []engine.PlayerID{engine.PlayerID(creator.String()), engine.PlayerID(opponent.String())})
	if err != nil {
		t.Fatal(err)
	}
	return session.Session{ID: uuid.New(), Game: tictactoe.Key, Creator: creator, Opponent: &opponent, State: state}
}

func cell(t *testing.T, row, col int) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(tictactoe.Cell{Row: row, Col: col})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStrangersWatchStartedGamesOnly(t *testing.T) {
	r := rules(t)
	creator, opponent, stranger := uuid.New(), uuid.New(), uuid.New()
	playing := started(t, r, creator, opponent)
	waiting := session.Session{ID: uuid.New(), Game: tictactoe.Key, Creator: creator, State: playing.State}
	cases := []struct {
		name  string
		game  session.Session
		user  uuid.UUID
		found bool
	}{
		{"creator of an open game", waiting, creator, true},
		{"stranger at an open game", waiting, stranger, false},
		{"player of a started game", playing, opponent, true},
		{"stranger at a started game", playing, stranger, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := r.Get(c.game, c.user, time.Now())
			if c.found && err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !c.found && !errors.Is(err, session.ErrNotFound) {
				t.Fatalf("Get = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStrangersCannotMove(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	game := started(t, r, creator, opponent)
	if _, _, err := r.Move(game, uuid.New(), 0, tictactoe.Key, cell(t, 0, 0), nil, time.Now()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("Move by a spectator = %v, want ErrNotFound", err)
	}
}

func TestFramesReplayEveryPosition(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	game := started(t, r, creator, opponent)
	moves := []session.MoveRecord{
		{Ply: 1, Player: creator, Data: cell(t, 0, 0)},
		{Ply: 2, Player: opponent, Data: cell(t, 1, 1)},
		{Ply: 3, Player: creator, Data: cell(t, 0, 1)},
	}
	replayed, err := frames(t.Context(), r, game, moves)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(replayed) != len(moves)+1 {
		t.Fatalf("got %d frames, want %d", len(replayed), len(moves)+1)
	}
	for ply, frame := range replayed {
		if frame.Ply != ply {
			t.Fatalf("frame %d is at ply %d", ply, frame.Ply)
		}
	}
	final, err := r.Replay(game, moves)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if string(replayed[len(replayed)-1].Data) != string(final.Data) {
		t.Fatal("last frame differs from the replayed position")
	}
	cells, _, err := tictactoe.Position(replayed[2])
	if err != nil {
		t.Fatal(err)
	}
	if cells[0] != 0 || cells[4] != 1 || cells[1] != engine.NoSeat {
		t.Fatalf("position after two moves = %v", cells)
	}
}

func TestFramesRejectGapsAndIllegalHistory(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	game := started(t, r, creator, opponent)
	cases := []struct {
		name  string
		moves []session.MoveRecord
		want  error
	}{
		{"missing ply", []session.MoveRecord{{Ply: 1, Player: creator, Data: cell(t, 0, 0)}, {Ply: 3, Player: creator, Data: cell(t, 0, 1)}}, session.ErrHistoryGap},
		{"same cell twice", []session.MoveRecord{{Ply: 1, Player: creator, Data: cell(t, 0, 0)}, {Ply: 2, Player: opponent, Data: cell(t, 0, 0)}}, tictactoe.ErrCellTaken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := frames(t.Context(), r, game, c.moves); !errors.Is(err, c.want) {
				t.Fatalf("Frames = %v, want %v", err, c.want)
			}
		})
	}
}

func TestFramesStopWhenTheRequestIsCancelled(t *testing.T) {
	r := rules(t)
	creator, opponent := uuid.New(), uuid.New()
	game := started(t, r, creator, opponent)
	moves := []session.MoveRecord{
		{Ply: 1, Player: creator, Data: cell(t, 0, 0)},
		{Ply: 2, Player: opponent, Data: cell(t, 1, 1)},
	}
	ctx, cancel := context.WithCancel(t.Context())
	seen := 0
	err := r.Frames(ctx, game, moves, func(engine.State) error {
		seen++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || seen != 1 {
		t.Fatalf("Frames after a cancel = %v with %d frames, want %v after 1", err, seen, context.Canceled)
	}
	stop := errors.New("stop")
	if err := r.Frames(t.Context(), game, moves, func(engine.State) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("Frames with a failing visitor = %v, want %v", err, stop)
	}
}

func TestReplayKeepsTheMoveLimitTheGameStartedWith(t *testing.T) {
	games := engine.NewRegistry()
	def := quoridor.Definition()
	def.MaxPlies = 4
	if err := games.Register(def); err != nil {
		t.Fatal(err)
	}
	r := session.NewRules(games)
	creator, opponent := uuid.New(), uuid.New()
	players := []engine.PlayerID{engine.PlayerID(creator.String()), engine.PlayerID(opponent.String())}
	shuffle := []string{"e2", "e8", "e1", "e9", "e2", "e8"}
	moves := make([]session.MoveRecord, len(shuffle))
	for i, move := range shuffle {
		player := creator
		if i%2 == 1 {
			player = opponent
		}
		data, err := json.Marshal(quoridor.MoveData{Move: move})
		if err != nil {
			t.Fatal(err)
		}
		moves[i] = session.MoveRecord{Ply: i + 1, Player: player, Data: data}
	}
	legacy := session.Session{ID: uuid.New(), Game: quoridor.Key, Creator: creator, Opponent: &opponent,
		State: engine.State{Game: quoridor.Key, Players: players, Ply: len(shuffle), Status: engine.StatusInProgress, Winner: engine.NoSeat}}
	replayed, err := frames(t.Context(), r, legacy, moves)
	if err != nil || len(replayed) != len(shuffle)+1 || replayed[len(shuffle)].Over() {
		t.Fatalf("Frames of a game without a limit = %d frames, %v", len(replayed), err)
	}
	if state, err := r.Replay(legacy, moves); err != nil || state.Ply != len(shuffle) || state.Over() {
		t.Fatalf("Replay of a game without a limit = ply %d over %v, %v", state.Ply, state.Over(), err)
	}
	capped := legacy
	capped.State.MaxPlies = 4
	if _, err := frames(t.Context(), r, capped, moves); !errors.Is(err, engine.ErrGameOver) {
		t.Fatalf("Frames past the limit = %v, want %v", err, engine.ErrGameOver)
	}
}

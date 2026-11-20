package session_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

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
	frames, err := r.Frames(game, moves)
	if err != nil {
		t.Fatalf("Frames: %v", err)
	}
	if len(frames) != len(moves)+1 {
		t.Fatalf("got %d frames, want %d", len(frames), len(moves)+1)
	}
	for ply, frame := range frames {
		if frame.Ply != ply {
			t.Fatalf("frame %d is at ply %d", ply, frame.Ply)
		}
	}
	final, err := r.Replay(game, moves)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if string(frames[len(frames)-1].Data) != string(final.Data) {
		t.Fatal("last frame differs from the replayed position")
	}
	cells, _, err := tictactoe.Position(frames[2])
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
			if _, err := r.Frames(game, c.moves); !errors.Is(err, c.want) {
				t.Fatalf("Frames = %v, want %v", err, c.want)
			}
		})
	}
}

package tictactoe_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
)

const (
	alice engine.PlayerID = "alice"
	bob   engine.PlayerID = "bob"
	carol engine.PlayerID = "carol"
)

// boardPayload mirrors the JSON the game stores. Declared here rather than
// exported from the package under test on purpose: the payload's shape is a
// stored contract, and a test that spells it out fails the day it changes
// silently.
type boardPayload struct {
	Cells [tictactoe.Size * tictactoe.Size]int `json:"cells"`
}

// cell is one step of a scripted game: who plays where is decided by whose turn
// it is, so only the square is scripted.
type cell struct{ row, col int }

func newGame(t *testing.T) (tictactoe.Game, engine.State) {
	t.Helper()

	game := tictactoe.New()
	return game, game.Init([]engine.PlayerID{alice, bob})
}

// play walks a scripted game, taking the player to move from the position
// itself, and returns the final position along with the moves it recorded — the
// same history the session layer would write to Postgres.
func play(t *testing.T, game tictactoe.Game, state engine.State, script []cell) (engine.State, []engine.Move) {
	t.Helper()

	history := make([]engine.Move, 0, len(script))
	for i, c := range script {
		player, ok := state.Current()
		if !ok {
			t.Fatalf("step %d (%d,%d): the game was already over", i, c.row, c.col)
		}

		move := tictactoe.NewMove(player, state.Ply, c.row, c.col)
		next, err := game.ApplyMove(state, move)
		if err != nil {
			t.Fatalf("step %d (%d,%d) by %s: %v", i, c.row, c.col, player, err)
		}

		history = append(history, move)
		state = next
	}

	return state, history
}

func decodeBoard(t *testing.T, state engine.State) boardPayload {
	t.Helper()

	var board boardPayload
	if err := json.Unmarshal(state.Data, &board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	return board
}

// winForFirstSeat: the first seat takes the top row while the second answers in
// the middle one.
var winForFirstSeat = []cell{
	{0, 0}, {1, 0},
	{0, 1}, {1, 1},
	{0, 2},
}

// drawnGame fills the board with nobody completing a line.
var drawnGame = []cell{
	{0, 0}, {1, 1},
	{0, 1}, {0, 2},
	{2, 0}, {1, 0},
	{2, 2}, {2, 1},
	{1, 2},
}

func TestInit(t *testing.T) {
	_, state := newGame(t)

	if state.Game != tictactoe.Key {
		t.Errorf("Game = %q, want %q", state.Game, tictactoe.Key)
	}
	if state.Status != engine.StatusInProgress {
		t.Errorf("Status = %q, want %q", state.Status, engine.StatusInProgress)
	}
	if state.Turn != 0 || state.Ply != 0 || state.Winner != engine.NoSeat {
		t.Errorf("opening position: turn %d, ply %d, winner %d", state.Turn, state.Ply, state.Winner)
	}

	for i, taken := range decodeBoard(t, state).Cells {
		if taken != -1 {
			t.Errorf("cell %d = %d in the opening position, want -1", i, taken)
		}
	}
}

func TestGameToAWin(t *testing.T) {
	game, state := newGame(t)

	final, history := play(t, game, state, winForFirstSeat)

	if len(history) != len(winForFirstSeat) {
		t.Fatalf("recorded %d moves, want %d", len(history), len(winForFirstSeat))
	}
	if final.Status != engine.StatusWin {
		t.Errorf("Status = %q, want %q", final.Status, engine.StatusWin)
	}
	if final.Winner != 0 {
		t.Errorf("Winner = %d, want seat 0", final.Winner)
	}
	if final.Ply != len(winForFirstSeat) {
		t.Errorf("Ply = %d, want %d", final.Ply, len(winForFirstSeat))
	}
	if final.Turn != engine.NoSeat {
		t.Errorf("a decided game still has seat %d to move", final.Turn)
	}

	over, winner := game.IsTerminal(final)
	if !over {
		t.Fatal("IsTerminal() said the decided game was still running")
	}
	if winner == nil || *winner != alice {
		t.Errorf("IsTerminal() winner = %v, want %q", winner, alice)
	}

	// Nobody can move any more, including the player who was about to.
	for _, player := range []engine.PlayerID{alice, bob} {
		if moves := game.ValidMoves(final, player); moves != nil {
			t.Errorf("ValidMoves(%s) after the win = %d moves, want none", player, len(moves))
		}
	}
}

func TestGameToADraw(t *testing.T) {
	game, state := newGame(t)

	final, _ := play(t, game, state, drawnGame)

	if final.Status != engine.StatusDraw {
		t.Fatalf("Status = %q, want %q", final.Status, engine.StatusDraw)
	}
	if final.Winner != engine.NoSeat {
		t.Errorf("Winner = %d, want %d", final.Winner, engine.NoSeat)
	}

	over, winner := game.IsTerminal(final)
	if !over {
		t.Fatal("IsTerminal() said the drawn game was still running")
	}
	if winner != nil {
		t.Errorf("IsTerminal() winner = %q, want none", *winner)
	}

	for i, taken := range decodeBoard(t, final).Cells {
		if taken == -1 {
			t.Errorf("cell %d is still empty in a drawn game", i)
		}
	}
}

func TestApplyMoveRejects(t *testing.T) {
	game, fresh := newGame(t)

	taken, _ := play(t, game, fresh, []cell{{1, 1}})
	decided, _ := play(t, game, fresh, winForFirstSeat)

	fromAnotherGame := fresh
	fromAnotherGame.Game = "chess"

	tests := []struct {
		name    string
		state   engine.State
		move    engine.Move
		wantErr error
	}{
		{
			name:    "a cell somebody already took",
			state:   taken,
			move:    tictactoe.NewMove(bob, taken.Ply, 1, 1),
			wantErr: tictactoe.ErrCellTaken,
		},
		{
			name:    "a row above the board",
			state:   fresh,
			move:    tictactoe.NewMove(alice, 0, -1, 0),
			wantErr: tictactoe.ErrCellOutOfBoard,
		},
		{
			name:    "a column past the board",
			state:   fresh,
			move:    tictactoe.NewMove(alice, 0, 0, tictactoe.Size),
			wantErr: tictactoe.ErrCellOutOfBoard,
		},
		{
			name:    "moving out of turn",
			state:   fresh,
			move:    tictactoe.NewMove(bob, 0, 2, 2),
			wantErr: engine.ErrOutOfTurn,
		},
		{
			name:    "a move after the game was decided",
			state:   decided,
			move:    tictactoe.NewMove(bob, decided.Ply, 2, 2),
			wantErr: engine.ErrGameOver,
		},
		{
			name:    "somebody who is not playing",
			state:   fresh,
			move:    tictactoe.NewMove(carol, 0, 0, 0),
			wantErr: engine.ErrPlayerNotInGame,
		},
		{
			name:    "a move computed against an older position",
			state:   taken,
			move:    tictactoe.NewMove(bob, 0, 2, 2),
			wantErr: engine.ErrStaleMove,
		},
		{
			name:    "a payload that is not a cell",
			state:   fresh,
			move:    engine.Move{Player: alice, Ply: 0, Data: json.RawMessage(`"e2e4"`)},
			wantErr: engine.ErrMoveMalformed,
		},
		{
			name:    "a position from another game",
			state:   fromAnotherGame,
			move:    tictactoe.NewMove(alice, 0, 0, 0),
			wantErr: engine.ErrGameMismatch,
		},
		{
			name:    "a position whose payload cannot be read",
			state:   engine.NewState(tictactoe.Key, []engine.PlayerID{alice, bob}, json.RawMessage(`{"cells":`)),
			move:    tictactoe.NewMove(alice, 0, 0, 0),
			wantErr: engine.ErrStateMalformed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := game.ApplyMove(tt.state, tt.move)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ApplyMove() error = %v, want %v", err, tt.wantErr)
			}
			if got.Game != "" || got.Data != nil {
				t.Errorf("a rejected move still returned a position: %+v", got)
			}
		})
	}
}

// ApplyMove is required to be a pure function (rules/go-services.md). Purity is
// checked rather than asserted in a comment: the session layer applies moves to
// cached positions it keeps using afterwards, so a write through the input would
// corrupt state nobody thought was in play.
func TestApplyMoveLeavesTheInputAlone(t *testing.T) {
	game, state := newGame(t)

	before, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal the position: %v", err)
	}

	next, err := game.ApplyMove(state, tictactoe.NewMove(alice, state.Ply, 1, 1))
	if err != nil {
		t.Fatalf("ApplyMove(): %v", err)
	}

	after, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal the position again: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("ApplyMove() changed the position it was given:\n before %s\n after  %s", before, after)
	}

	// Equal contents are not enough: sharing the payload's backing array would
	// leave the next move free to write into the previous position.
	if len(next.Data) > 0 && len(state.Data) > 0 && &next.Data[0] == &state.Data[0] {
		t.Error("the new position shares its payload with the old one")
	}

	// The same, one level up: the recorded history must not shift either.
	final, history := play(t, game, state, drawnGame)
	if final.Ply != len(drawnGame) {
		t.Fatalf("Ply = %d, want %d", final.Ply, len(drawnGame))
	}
	for i, move := range history {
		if move.Ply != i {
			t.Errorf("recorded move %d carries ply %d", i, move.Ply)
		}
	}
}

func TestValidMoves(t *testing.T) {
	game, state := newGame(t)

	opening := game.ValidMoves(state, alice)
	if len(opening) != tictactoe.Size*tictactoe.Size {
		t.Fatalf("ValidMoves() in the opening position = %d, want %d", len(opening), tictactoe.Size*tictactoe.Size)
	}

	// Every suggested move must be one ApplyMove actually accepts — the client
	// highlights these and is told not to duplicate the rules.
	for _, move := range opening {
		if move.Ply != state.Ply {
			t.Errorf("suggested move carries ply %d, want %d", move.Ply, state.Ply)
		}
		if _, err := game.ApplyMove(state, move); err != nil {
			t.Errorf("ApplyMove() refused a move it suggested: %v", err)
		}
	}

	if moves := game.ValidMoves(state, bob); moves != nil {
		t.Errorf("ValidMoves() for the seat not to move = %d, want none", len(moves))
	}
	if moves := game.ValidMoves(state, carol); moves != nil {
		t.Errorf("ValidMoves() for somebody not playing = %d, want none", len(moves))
	}

	next, _ := play(t, game, state, []cell{{1, 1}})
	if moves := game.ValidMoves(next, bob); len(moves) != tictactoe.Size*tictactoe.Size-1 {
		t.Errorf("ValidMoves() after one move = %d, want %d", len(moves), tictactoe.Size*tictactoe.Size-1)
	}

	broken := engine.NewState(tictactoe.Key, []engine.PlayerID{alice, bob}, json.RawMessage(`{"cells":`))
	if moves := game.ValidMoves(broken, alice); moves != nil {
		t.Errorf("ValidMoves() on an unreadable position = %d, want none", len(moves))
	}
}

// The session layer stores moves, not positions, and rebuilds state by replaying
// them. This is that path end to end, with the history pushed through JSON the
// way a Postgres round trip would push it.
func TestReplayingAStoredHistoryRebuildsTheSamePosition(t *testing.T) {
	game, opening := newGame(t)

	direct, history := play(t, game, opening, winForFirstSeat)

	replayed := game.Init([]engine.PlayerID{alice, bob})
	for i, move := range history {
		stored, err := json.Marshal(move)
		if err != nil {
			t.Fatalf("store move %d: %v", i, err)
		}

		var loaded engine.Move
		if err := json.Unmarshal(stored, &loaded); err != nil {
			t.Fatalf("load move %d: %v", i, err)
		}

		if replayed, err = game.ApplyMove(replayed, loaded); err != nil {
			t.Fatalf("replay move %d: %v", i, err)
		}
	}

	want, err := json.Marshal(direct)
	if err != nil {
		t.Fatalf("marshal the position played directly: %v", err)
	}
	got, err := json.Marshal(replayed)
	if err != nil {
		t.Fatalf("marshal the replayed position: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("replay produced a different position:\n played   %s\n replayed %s", want, got)
	}
}

// The point of the registry is that a game is reached without naming its type.
// A game played entirely through it is the check that tic-tac-toe is reachable
// that way, and that its Definition matches what it actually needs.
func TestPlayingThroughTheRegistry(t *testing.T) {
	registry := engine.NewRegistry()
	if err := registry.Register(tictactoe.Definition()); err != nil {
		t.Fatalf("Register(): %v", err)
	}

	state, err := registry.NewState(tictactoe.Key, []engine.PlayerID{alice, bob})
	if err != nil {
		t.Fatalf("NewState(): %v", err)
	}

	game, err := registry.Get(tictactoe.Key)
	if err != nil {
		t.Fatalf("Get(): %v", err)
	}

	for _, c := range winForFirstSeat {
		player, ok := state.Current()
		if !ok {
			t.Fatal("the game ended before the script did")
		}
		if state, err = game.ApplyMove(state, tictactoe.NewMove(player, state.Ply, c.row, c.col)); err != nil {
			t.Fatalf("ApplyMove(): %v", err)
		}
	}

	over, winner := game.IsTerminal(state)
	if !over || winner == nil || *winner != alice {
		t.Errorf("IsTerminal() = %v, %v; want true and %q", over, winner, alice)
	}

	// A three-player roster is refused before Init ever sees it.
	if _, err := registry.NewState(tictactoe.Key, []engine.PlayerID{alice, bob, carol}); !errors.Is(err, engine.ErrPlayerCount) {
		t.Errorf("NewState() with three players = %v, want %v", err, engine.ErrPlayerCount)
	}
}

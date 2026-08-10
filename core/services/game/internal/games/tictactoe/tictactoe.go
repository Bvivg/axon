// Package tictactoe implements 3x3 tic-tac-toe behind engine.Game.
//
// It is the first game in the service and therefore also the worked example of
// what a game owes the engine: decode the payload, check its own rules, hand
// back a new position. Everything that is not specific to tic-tac-toe — whose
// turn it is, whether the game is over, whether the mover is even playing — is
// engine.Precheck's, not this package's.
package tictactoe

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

// Key is how the game is addressed in engine.Registry and named in stored
// sessions.
const Key engine.Key = "tictactoe"

// Rules specific to this game fail with these; the shared ones live in engine.
var (
	// ErrCellTaken is returned for a move onto an occupied cell.
	ErrCellTaken = errors.New("tictactoe: cell is already taken")

	// ErrCellOutOfBoard is returned for a row or column outside the board.
	ErrCellOutOfBoard = errors.New("tictactoe: cell is outside the board")
)

// Cell is the game-specific payload of a move: a zero-based row and column.
// Exported because it is the shape clients send and the shape stored in the
// move history.
type Cell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

// Game implements engine.Game.
//
// It holds no state: every method is a pure function of its arguments, so a
// single value is safe to register once and share across every session in the
// process.
type Game struct{}

// Interchangeability through the registry is the whole point of the interface,
// so the compiler checks it here rather than at the first wiring.
var _ engine.Game = Game{}

// New returns the tic-tac-toe implementation.
func New() Game { return Game{} }

// Definition describes the game for engine.Registry. It is a package function
// rather than a method on Game because Game must carry nothing beyond the four
// interface methods.
func Definition() engine.Definition {
	return engine.Definition{
		Key:        Key,
		MinPlayers: 2,
		MaxPlayers: 2,
		Game:       New(),
	}
}

// NewMove builds a move for the given cell. Clients and tests use it so the
// payload's JSON shape stays inside this package; the WebSocket layer, which
// receives the payload already encoded, passes it through untouched instead.
func NewMove(player engine.PlayerID, ply, row, col int) engine.Move {
	// A struct of two ints always marshals. Were that ever untrue the empty
	// payload left behind would be refused by ApplyMove, which is the right
	// answer anyway.
	data, _ := json.Marshal(Cell{Row: row, Col: col})

	return engine.Move{Player: player, Ply: ply, Data: data}
}

// Init returns the opening position: an empty board with the first seat to move.
// The roster is trusted — engine.Registry.NewState is what checks it.
func (Game) Init(players []engine.PlayerID) engine.State {
	data, _ := json.Marshal(newBoard())

	return engine.NewState(Key, players, data)
}

// ApplyMove returns the position that follows state once move is played.
//
// It is a pure function: no I/O, and the position it is given is never modified.
// The board it works on is decoded fresh out of the payload, so there is nothing
// shared with the input to write through in the first place.
func (Game) ApplyMove(state engine.State, move engine.Move) (engine.State, error) {
	seat, err := engine.Precheck(Key, state, move)
	if err != nil {
		return engine.State{}, err
	}

	var b board
	if err := engine.DecodeState(state, &b); err != nil {
		return engine.State{}, err
	}

	var cell Cell
	if err := engine.DecodeMove(move, &cell); err != nil {
		return engine.State{}, err
	}

	if !onBoard(cell.Row, cell.Col) {
		return engine.State{}, fmt.Errorf("%w: row %d, column %d", ErrCellOutOfBoard, cell.Row, cell.Col)
	}
	if b.Cells[index(cell.Row, cell.Col)] != emptyCell {
		return engine.State{}, fmt.Errorf("%w: row %d, column %d", ErrCellTaken, cell.Row, cell.Col)
	}

	b.Cells[index(cell.Row, cell.Col)] = seat

	data, err := json.Marshal(b)
	if err != nil {
		return engine.State{}, fmt.Errorf("tictactoe: encode board: %w", err)
	}

	// The turn is passed by seat arithmetic rather than by flipping a flag: the
	// same expression is what a three-player game would use, and it cannot go
	// out of range for any roster.
	next := state.Advance(data, (seat+1)%len(state.Players))

	switch {
	case b.wins(seat):
		next = next.Finish(seat)
	case b.full():
		next = next.Finish(engine.NoSeat)
	}

	return next, nil
}

// ValidMoves lists the empty cells when it is the player's turn, and nothing
// otherwise. A position whose payload cannot be read yields no moves: the caller
// has no error to return, and ApplyMove reports the same corruption properly.
func (Game) ValidMoves(state engine.State, player engine.PlayerID) []engine.Move {
	if state.Over() {
		return nil
	}

	seat, ok := state.Seat(player)
	if !ok || seat != state.Turn {
		return nil
	}

	var b board
	if err := engine.DecodeState(state, &b); err != nil {
		return nil
	}

	moves := make([]engine.Move, 0, len(b.Cells))
	for i, cell := range b.Cells {
		if cell == emptyCell {
			moves = append(moves, NewMove(player, state.Ply, i/Size, i%Size))
		}
	}
	if len(moves) == 0 {
		return nil
	}

	return moves
}

// IsTerminal reads the outcome recorded in the position.
//
// It does not re-examine the board on purpose. ApplyMove is the only way to
// reach a terminal position — replay goes through it too — and it decides the
// outcome there; deciding it again here would be the same rule written twice,
// free to disagree with itself.
func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

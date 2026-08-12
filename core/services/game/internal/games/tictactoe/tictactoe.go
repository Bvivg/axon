package tictactoe

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const Key engine.Key = "tictactoe"

var (
	ErrCellTaken = errors.New("tictactoe: cell is already taken")

	ErrCellOutOfBoard = errors.New("tictactoe: cell is outside the board")
)

type Cell struct {
	Row int `json:"row"`
	Col int `json:"col"`
}

type Game struct{}

var _ engine.Game = Game{}

func New() Game { return Game{} }

func Definition() engine.Definition {
	return engine.Definition{
		Key:        Key,
		MinPlayers: 2,
		MaxPlayers: 2,
		Game:       New(),
	}
}

func NewMove(player engine.PlayerID, ply, row, col int) engine.Move {

	data, _ := json.Marshal(Cell{Row: row, Col: col})

	return engine.Move{Player: player, Ply: ply, Data: data}
}

func (Game) Init(players []engine.PlayerID) engine.State {
	data, _ := json.Marshal(newBoard())

	return engine.NewState(Key, players, data)
}

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

	next := state.Advance(data, (seat+1)%len(state.Players))

	switch {
	case b.wins(seat):
		next = next.Finish(seat)
	case b.full():
		next = next.Finish(engine.NoSeat)
	}

	return next, nil
}

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

func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

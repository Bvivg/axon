package checkers

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const Key engine.Key = "checkers"

const (
	QuietLimit = 80
	MaxPlies   = 500
)

var ErrIllegalMove = errors.New("checkers: illegal move")

type End string

const (
	Ongoing    End = ""
	NoMoves    End = "no_moves"
	FortyMoves End = "forty_moves"
	MoveLimit  End = "move_limit"
)

type MoveData struct {
	Move string `json:"move"`
}

type data struct {
	Board string   `json:"board"`
	Moves []string `json:"moves"`
	Quiet int      `json:"quiet"`
	End   End      `json:"end,omitempty"`
}

type Game struct{}

var _ engine.Game = Game{}

func New() Game { return Game{} }

func Definition() engine.Definition {
	return engine.Definition{Key: Key, MinPlayers: 2, MaxPlayers: 2, Game: New(), TimeLimit: 10 * time.Minute}
}

func NewMove(player engine.PlayerID, ply int, move string) engine.Move {
	raw, _ := json.Marshal(MoveData{Move: move})
	return engine.Move{Player: player, Ply: ply, Data: raw}
}

func (Game) Init(players []engine.PlayerID) engine.State {
	raw, _ := json.Marshal(data{Board: NewBoard().String(), Moves: []string{}})
	return engine.NewState(Key, players, raw)
}

func decode(state engine.State) (data, Board, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return data{}, Board{}, err
	}
	b, err := ParseBoard(d.Board)
	if err != nil {
		return data{}, Board{}, fmt.Errorf("%w: %w", engine.ErrStateMalformed, err)
	}
	if d.Quiet < 0 || d.Quiet > QuietLimit || len(d.Moves) > MaxPlies {
		return data{}, Board{}, fmt.Errorf("%w: quiet %d, %d moves", engine.ErrStateMalformed, d.Quiet, len(d.Moves))
	}
	return d, b, nil
}

func (Game) ApplyMove(state engine.State, move engine.Move) (engine.State, error) {
	seat, err := engine.Precheck(Key, state, move)
	if err != nil {
		return engine.State{}, err
	}
	d, b, err := decode(state)
	if err != nil {
		return engine.State{}, err
	}
	var in MoveData
	if err := engine.DecodeMove(move, &in); err != nil {
		return engine.State{}, err
	}
	want, err := ParseMove(in.Move)
	if err != nil {
		return engine.State{}, err
	}
	chosen, ok := b.Legal(seat, want)
	if !ok {
		return engine.State{}, fmt.Errorf("%w: %s", ErrIllegalMove, in.Move)
	}
	quiet := d.Quiet + 1
	if chosen.Jump || !b[chosen.From()].King() {
		quiet = 0
	}
	next := b.Play(chosen)
	out := data{Board: next.String(), Moves: append(append(make([]string, 0, len(d.Moves)+1), d.Moves...), chosen.String()), Quiet: quiet}
	switch {
	case len(next.LegalMoves(1-seat)) == 0:
		out.End = NoMoves
	case quiet >= QuietLimit:
		out.End = FortyMoves
	case state.Ply+1 >= MaxPlies:
		out.End = MoveLimit
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return engine.State{}, fmt.Errorf("checkers: encode state: %w", err)
	}
	result := state.Advance(raw, 1-seat)
	switch {
	case out.End == NoMoves:
		result = result.Finish(seat)
	case out.End != Ongoing && !result.Over():
		result = result.Finish(engine.NoSeat)
	}
	return result, nil
}

func (Game) ValidMoves(state engine.State, player engine.PlayerID) []engine.Move {
	if state.Over() {
		return nil
	}
	seat, ok := state.Seat(player)
	if !ok || seat != state.Turn {
		return nil
	}
	_, b, err := decode(state)
	if err != nil {
		return nil
	}
	legal := b.LegalMoves(seat)
	moves := make([]engine.Move, 0, len(legal))
	for _, m := range legal {
		moves = append(moves, NewMove(player, state.Ply, m.String()))
	}
	return moves
}

func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

type Snapshot struct {
	Board    string
	Moves    []string
	LastMove string
	Legal    []string
	Quiet    int
	End      End
}

func View(state engine.State) (Snapshot, error) {
	d, b, err := decode(state)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Board: d.Board, Moves: d.Moves, Quiet: d.Quiet, End: d.End}
	if n := len(d.Moves); n > 0 {
		snap.LastMove = d.Moves[n-1]
	}
	if !state.Over() && state.Turn >= 0 && state.Turn < 2 {
		for _, m := range b.LegalMoves(state.Turn) {
			snap.Legal = append(snap.Legal, m.String())
		}
	}
	return snap, nil
}

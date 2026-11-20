package quoridor

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const Key engine.Key = "quoridor"

var ErrIllegalMove = errors.New("quoridor: illegal move")

type MoveData struct {
	Move string `json:"move"`
}

type data struct {
	Pawns [2]string `json:"pawns"`
	Walls []string  `json:"walls"`
	Left  [2]int    `json:"left"`
	Moves []string  `json:"moves"`
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

func encode(b Board, moves []string) data {
	walls := make([]string, 0, len(b.walls))
	for _, w := range b.Walls() {
		walls = append(walls, w.String())
	}
	sort.Strings(walls)
	return data{Pawns: [2]string{b.Pawns[0].String(), b.Pawns[1].String()}, Walls: walls, Left: b.Left, Moves: moves}
}

func (Game) Init(players []engine.PlayerID) engine.State {
	raw, _ := json.Marshal(encode(NewBoard(), []string{}))
	return engine.NewState(Key, players, raw)
}

func decode(state engine.State) (data, Board, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return data{}, Board{}, err
	}
	b := NewBoard()
	b.Left = d.Left
	for seat, raw := range d.Pawns {
		m, err := ParseMove(raw)
		if err != nil || m.Wall != nil {
			return data{}, Board{}, fmt.Errorf("%w: pawn %q", engine.ErrStateMalformed, raw)
		}
		b.Pawns[seat] = m.To
	}
	for _, raw := range d.Walls {
		m, err := ParseMove(raw)
		if err != nil || m.Wall == nil {
			return data{}, Board{}, fmt.Errorf("%w: wall %q", engine.ErrStateMalformed, raw)
		}
		b.walls[*m.Wall] = true
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
	m, err := ParseMove(in.Move)
	if err != nil {
		return engine.State{}, err
	}
	if !b.Legal(seat, m) {
		return engine.State{}, fmt.Errorf("%w: %s", ErrIllegalMove, in.Move)
	}
	next := b.Play(seat, m)
	raw, err := json.Marshal(encode(next, append(append([]string{}, d.Moves...), m.String())))
	if err != nil {
		return engine.State{}, fmt.Errorf("quoridor: encode state: %w", err)
	}
	result := state.Advance(raw, 1-seat)
	if next.Pawns[seat].Row() == GoalRow(seat) {
		result = result.Finish(seat)
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
	Pawns    []string
	Walls    []string
	Left     []uint32
	Moves    []string
	LastMove string
	Legal    []string
}

func View(state engine.State) (Snapshot, error) {
	d, b, err := decode(state)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Pawns: d.Pawns[:], Walls: d.Walls, Left: []uint32{uint32(d.Left[0]), uint32(d.Left[1])}, Moves: d.Moves}
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

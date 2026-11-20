package chess

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const Key engine.Key = "chess"

var ErrIllegalMove = errors.New("chess: illegal move")

type MoveData struct {
	UCI string `json:"uci"`
}

type data struct {
	FEN       string   `json:"fen"`
	Moves     []string `json:"moves"`
	SAN       []string `json:"san"`
	Positions []string `json:"positions"`
	End       End      `json:"end,omitempty"`
}

type Game struct{}

var _ engine.Game = Game{}

func New() Game { return Game{} }

func Definition() engine.Definition {
	return engine.Definition{Key: Key, MinPlayers: 2, MaxPlayers: 2, Game: New(), TimeLimit: 10 * time.Minute}
}

func NewMove(player engine.PlayerID, ply int, uci string) engine.Move {
	raw, _ := json.Marshal(MoveData{UCI: uci})
	return engine.Move{Player: player, Ply: ply, Data: raw}
}

func (Game) Init(players []engine.PlayerID) engine.State {
	start, _ := ParseFEN(StartFEN)
	raw, _ := json.Marshal(data{FEN: StartFEN, Moves: []string{}, SAN: []string{}, Positions: []string{start.RepetitionKey()}})
	return engine.NewState(Key, players, raw)
}

func decode(state engine.State) (data, Position, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return data{}, Position{}, err
	}
	pos, err := ParseFEN(d.FEN)
	if err != nil {
		return data{}, Position{}, fmt.Errorf("%w: %w", engine.ErrStateMalformed, err)
	}
	return d, pos, nil
}

func (Game) ApplyMove(state engine.State, move engine.Move) (engine.State, error) {
	seat, err := engine.Precheck(Key, state, move)
	if err != nil {
		return engine.State{}, err
	}
	d, pos, err := decode(state)
	if err != nil {
		return engine.State{}, err
	}
	if Color(seat) != pos.Turn {
		return engine.State{}, fmt.Errorf("%w: seat %d, position to move %d", engine.ErrStateMalformed, seat, pos.Turn)
	}
	var in MoveData
	if err := engine.DecodeMove(move, &in); err != nil {
		return engine.State{}, err
	}
	want, err := ParseUCI(in.UCI)
	if err != nil {
		return engine.State{}, err
	}
	var chosen *Move
	for _, m := range pos.LegalMoves() {
		if m == want {
			chosen = &m
			break
		}
	}
	if chosen == nil {
		return engine.State{}, fmt.Errorf("%w: %s", ErrIllegalMove, in.UCI)
	}
	san := pos.SAN(*chosen)
	next := pos.Play(*chosen)
	positions := d.Positions
	if next.Halfmove == 0 {
		positions = nil
	}
	positions = append(append([]string{}, positions...), next.RepetitionKey())
	out := data{
		FEN:       next.FEN(),
		Moves:     append(append([]string{}, d.Moves...), chosen.UCI()),
		SAN:       append(append([]string{}, d.SAN...), san),
		Positions: positions,
		End:       Outcome(next, positions),
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return engine.State{}, fmt.Errorf("chess: encode state: %w", err)
	}
	result := state.Advance(raw, int(next.Turn))
	switch out.End {
	case Ongoing:
	case Checkmate:
		result = result.Finish(seat)
	default:
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
	_, pos, err := decode(state)
	if err != nil {
		return nil
	}
	legal := pos.LegalMoves()
	if len(legal) == 0 {
		return nil
	}
	moves := make([]engine.Move, 0, len(legal))
	for _, m := range legal {
		moves = append(moves, NewMove(player, state.Ply, m.UCI()))
	}
	return moves
}

func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

type Snapshot struct {
	FEN      string
	SAN      []string
	LastMove string
	Check    bool
	Legal    []string
	End      End
}

func View(state engine.State) (Snapshot, error) {
	d, pos, err := decode(state)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{FEN: d.FEN, SAN: d.SAN, Check: pos.InCheck(), End: d.End}
	if n := len(d.Moves); n > 0 {
		snap.LastMove = d.Moves[n-1]
	}
	if !state.Over() {
		for _, m := range pos.LegalMoves() {
			snap.Legal = append(snap.Legal, m.UCI())
		}
	}
	return snap, nil
}

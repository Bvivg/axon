package durak

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const (
	Key      engine.Key = "durak"
	MaxPlies            = 1000
)

var (
	ErrIllegalMove = errors.New("durak: illegal move")
	ErrBadMove     = errors.New("durak: malformed move")
)

type End string

const (
	Ongoing   End = ""
	Lost      End = "durak"
	Drawn     End = "draw"
	MoveLimit End = "move_limit"
)

type MoveData struct {
	Move string `json:"move"`
}

type pair struct {
	Attack  string `json:"attack"`
	Defense string `json:"defense,omitempty"`
}

type data struct {
	Hands    [2][]string `json:"hands"`
	Held     [2]int      `json:"held"`
	Stock    []string    `json:"stock"`
	Left     int         `json:"left"`
	Trump    string      `json:"trump"`
	Table    []pair      `json:"table"`
	Discard  []string    `json:"discard"`
	Attacker int         `json:"attacker"`
	Limit    int         `json:"limit"`
	Taking   bool        `json:"taking"`
	Moves    []string    `json:"moves"`
	End      End         `json:"end,omitempty"`
	Viewer   *int        `json:"viewer,omitempty"`
}

type Game struct{}

var (
	_ engine.Game   = Game{}
	_ engine.Seeded = Game{}
	_ engine.Viewer = Game{}
)

func New() Game { return Game{} }

func Definition() engine.Definition {
	return engine.Definition{Key: Key, MinPlayers: 2, MaxPlayers: 2, Game: New(), TimeLimit: 10 * time.Minute}
}

func NewMove(player engine.PlayerID, ply int, move string) engine.Move {
	raw, _ := json.Marshal(MoveData{Move: move})
	return engine.Move{Player: player, Ply: ply, Data: raw}
}

func encode(p Position, moves []string, end End) data {
	table := make([]pair, len(p.Table))
	for i, item := range p.Table {
		table[i].Attack = item.Attack.String()
		if item.Covered {
			table[i].Defense = item.Defense.String()
		}
	}
	return data{
		Hands: [2][]string{names(p.Hands[0]), names(p.Hands[1])}, Held: [2]int{len(p.Hands[0]), len(p.Hands[1])},
		Stock: names(p.Stock), Left: len(p.Stock), Trump: p.Trump.String(), Table: table, Discard: names(p.Discard),
		Attacker: p.Attacker, Limit: p.Limit, Taking: p.Taking, Moves: moves, End: end,
	}
}

func parseTable(items []pair) ([]Pair, error) {
	var out []Pair
	for _, item := range items {
		attack, err := ParseCard(item.Attack)
		if err != nil {
			return nil, err
		}
		next := Pair{Attack: attack}
		if item.Defense != "" {
			if next.Defense, err = ParseCard(item.Defense); err != nil {
				return nil, err
			}
			next.Covered = true
		}
		out = append(out, next)
	}
	return out, nil
}

func decode(state engine.State) (data, Position, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return data{}, Position{}, err
	}
	if d.Viewer != nil {
		return data{}, Position{}, fmt.Errorf("%w: a player's view is not a playable position", engine.ErrStateMalformed)
	}
	malformed := func(what string) error { return fmt.Errorf("%w: %s", engine.ErrStateMalformed, what) }
	var p Position
	var err error
	for seat := range d.Hands {
		if p.Hands[seat], err = parseCards(d.Hands[seat]); err != nil {
			return data{}, Position{}, malformed("hand")
		}
	}
	if p.Stock, err = parseCards(d.Stock); err != nil {
		return data{}, Position{}, malformed("stock")
	}
	if p.Discard, err = parseCards(d.Discard); err != nil {
		return data{}, Position{}, malformed("discard")
	}
	if p.Trump, err = ParseCard(d.Trump); err != nil {
		return data{}, Position{}, malformed("trump")
	}
	if p.Table, err = parseTable(d.Table); err != nil {
		return data{}, Position{}, malformed("table")
	}
	if d.Attacker != 0 && d.Attacker != 1 || d.Limit < 0 || d.Limit > HandSize || len(d.Table) > d.Limit {
		return data{}, Position{}, malformed("bout")
	}
	p.Attacker, p.Limit, p.Taking = d.Attacker, d.Limit, d.Taking
	cards := p.Cards()
	slices.SortFunc(cards, func(a, b Card) int { return int(a.Suit)*16 + int(a.Rank) - int(b.Suit)*16 - int(b.Rank) })
	if len(cards) != DeckSize || len(slices.Compact(cards)) != DeckSize {
		return data{}, Position{}, malformed("deck")
	}
	return d, p, nil
}

func (g Game) Init(players []engine.PlayerID) engine.State {
	return g.InitSeeded(players, 0)
}

func (Game) InitSeeded(players []engine.PlayerID, seed uint64) engine.State {
	p := Deal(seed)
	raw, _ := json.Marshal(encode(p, []string{}, Ongoing))
	state := engine.NewState(Key, players, raw)
	state.Turn, state.Seed = p.Turn(), seed
	return state
}

func (Game) ApplyMove(state engine.State, move engine.Move) (engine.State, error) {
	seat, err := engine.Precheck(Key, state, move)
	if err != nil {
		return engine.State{}, err
	}
	d, p, err := decode(state)
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
	next, err := p.Play(seat, m)
	if err != nil {
		return engine.State{}, err
	}
	over, winner := next.Outcome()
	end := Ongoing
	switch {
	case over && winner == engine.NoSeat:
		end = Drawn
	case over:
		end = Lost
	case state.Ply+1 >= MaxPlies:
		over, winner, end = true, engine.NoSeat, MoveLimit
	}
	raw, err := json.Marshal(encode(next, append(slices.Clone(d.Moves), m.String()), end))
	if err != nil {
		return engine.State{}, fmt.Errorf("durak: encode state: %w", err)
	}
	result := state.Advance(raw, next.Turn())
	if over {
		result = result.Finish(winner)
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
	_, p, err := decode(state)
	if err != nil || p.Turn() != seat {
		return nil
	}
	legal := p.Legal()
	moves := make([]engine.Move, 0, len(legal))
	for _, m := range legal {
		moves = append(moves, NewMove(player, state.Ply, m.String()))
	}
	return moves
}

func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

func (Game) View(state engine.State, viewer engine.PlayerID) engine.State {
	state.Seed = 0
	if state.Over() {
		return state
	}
	var d data
	if engine.DecodeState(state, &d) != nil {
		state.Data = nil
		return state
	}
	seat, ok := state.Seat(viewer)
	if !ok {
		seat = engine.NoSeat
	}
	for s := range d.Hands {
		if s != seat {
			d.Hands[s] = nil
		}
	}
	d.Stock, d.Viewer = nil, &seat
	raw, err := json.Marshal(d)
	if err != nil {
		state.Data = nil
		return state
	}
	state.Data = raw
	return state
}

type Hand struct {
	Cards  []string
	Count  int
	Hidden bool
}

type TablePair struct {
	Attack  string
	Defense string
}

type Snapshot struct {
	Hands    [2]Hand
	Trump    string
	Stock    int
	Table    []TablePair
	Discard  int
	Attacker int
	Taking   bool
	Limit    int
	Moves    []string
	LastMove string
	Legal    []string
	End      End
}

func Describe(state engine.State) (Snapshot, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Trump: d.Trump, Stock: d.Left, Discard: len(d.Discard), Attacker: d.Attacker, Taking: d.Taking, Limit: d.Limit, Moves: d.Moves, End: d.End}
	for seat := range d.Hands {
		visible := d.Viewer == nil || *d.Viewer == seat
		snap.Hands[seat] = Hand{Count: d.Held[seat], Hidden: !visible}
		if visible {
			snap.Hands[seat].Cards = d.Hands[seat]
		}
	}
	for _, item := range d.Table {
		snap.Table = append(snap.Table, TablePair{Attack: item.Attack, Defense: item.Defense})
	}
	if n := len(d.Moves); n > 0 {
		snap.LastMove = d.Moves[n-1]
	}
	if state.Over() || d.Viewer == nil || *d.Viewer != state.Turn || state.Turn < 0 || state.Turn > 1 {
		return snap, nil
	}
	p := Position{Attacker: d.Attacker, Limit: d.Limit, Taking: d.Taking}
	var err error
	if p.Trump, err = ParseCard(d.Trump); err != nil {
		return Snapshot{}, fmt.Errorf("%w: trump", engine.ErrStateMalformed)
	}
	if p.Hands[state.Turn], err = parseCards(d.Hands[state.Turn]); err != nil {
		return Snapshot{}, fmt.Errorf("%w: hand", engine.ErrStateMalformed)
	}
	if p.Table, err = parseTable(d.Table); err != nil {
		return Snapshot{}, fmt.Errorf("%w: table", engine.ErrStateMalformed)
	}
	if p.Turn() != state.Turn {
		return snap, nil
	}
	for _, m := range p.Legal() {
		snap.Legal = append(snap.Legal, m.String())
	}
	return snap, nil
}

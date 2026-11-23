package durak

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const Key engine.Key = "durak"

const MaxPlies = 1000

var (
	ErrIllegalMove = errors.New("durak: illegal move")
	ErrBadMove     = errors.New("durak: malformed move")
)

type End string

const (
	Ongoing End = ""
	Lost    End = "durak"
	Drawn   End = "draw"
)

type MoveData struct {
	Move string `json:"move"`
}

type pair struct {
	Attack  string   `json:"attack"`
	Defense string   `json:"defense,omitempty"`
	By      *int     `json:"by,omitempty"`
	Cheat   []string `json:"cheat,omitempty"`
	Checked []string `json:"checked,omitempty"`
}

type data struct {
	Hands    [][]string `json:"hands"`
	Held     []int      `json:"held"`
	Stock    []string   `json:"stock"`
	Left     int        `json:"left"`
	Trump    string     `json:"trump"`
	Table    []pair     `json:"table"`
	Discard  []string   `json:"discard"`
	Attacker int        `json:"attacker"`
	Limit    int        `json:"limit"`
	Taking   bool       `json:"taking"`
	Moves    []string   `json:"moves"`
	End      End        `json:"end,omitempty"`
	Viewer   *int       `json:"viewer,omitempty"`
	Options  *Options   `json:"options,omitempty"`
	Passed   []int      `json:"passed,omitempty"`
	Places   []int      `json:"places,omitempty"`
}

type Game struct{}

var (
	_ engine.Game         = Game{}
	_ engine.Seeded       = Game{}
	_ engine.Viewer       = Game{}
	_ engine.Configurable = Game{}
	_ engine.Botted       = Game{}
)

func New() Game { return Game{} }

func Definition() engine.Definition {
	return engine.Definition{Key: Key, MinPlayers: MinSeats, MaxPlayers: MaxSeats, Game: New(), TimeLimit: 10 * time.Minute, MaxPlies: MaxPlies}
}

func NewMove(player engine.PlayerID, ply int, move string) engine.Move {
	raw, _ := json.Marshal(MoveData{Move: move})
	return engine.Move{Player: player, Ply: ply, Data: raw}
}

func marked(pair Pair, mark Mark) []string {
	var out []string
	if mark&MarkAttack != 0 {
		out = append(out, pair.Attack.String())
	}
	if mark&MarkDefense != 0 && pair.Covered {
		out = append(out, pair.Defense.String())
	}
	return out
}

func encode(p Position, moves []string, end End) data {
	table := make([]pair, len(p.Table))
	for i, item := range p.Table {
		by := item.By
		table[i] = pair{Attack: item.Attack.String(), By: &by, Cheat: marked(item, item.Cheat), Checked: marked(item, item.Checked)}
		if item.Covered {
			table[i].Defense = item.Defense.String()
		}
	}
	hands := make([][]string, len(p.Hands))
	held := make([]int, len(p.Hands))
	for seat, hand := range p.Hands {
		hands[seat], held[seat] = names(hand), len(hand)
	}
	options := p.Options
	return data{
		Hands: hands, Held: held, Stock: names(p.Stock), Left: len(p.Stock), Trump: p.Trump.String(), Table: table,
		Discard: names(p.Discard), Attacker: p.Attacker, Limit: p.Limit, Taking: p.Taking, Moves: moves, End: end,
		Options: &options, Passed: slices.Clone(p.Passed), Places: slices.Clone(p.Places),
	}
}

func parseMarks(raw []string, item Pair) (Mark, error) {
	var mark Mark
	for _, name := range raw {
		c, err := ParseCard(name)
		switch {
		case err != nil:
			return 0, err
		case c == item.Attack:
			mark |= MarkAttack
		case item.Covered && c == item.Defense:
			mark |= MarkDefense
		default:
			return 0, fmt.Errorf("%w: %s is not in this pair", ErrBadMove, name)
		}
	}
	return mark, nil
}

func parseTable(items []pair, attacker int) ([]Pair, error) {
	var out []Pair
	for _, item := range items {
		attack, err := ParseCard(item.Attack)
		if err != nil {
			return nil, err
		}
		next := Pair{Attack: attack, By: attacker}
		if item.By != nil {
			next.By = *item.By
		}
		if item.Defense != "" {
			if next.Defense, err = ParseCard(item.Defense); err != nil {
				return nil, err
			}
			next.Covered = true
		}
		if next.Cheat, err = parseMarks(item.Cheat, next); err != nil {
			return nil, err
		}
		if next.Checked, err = parseMarks(item.Checked, next); err != nil {
			return nil, err
		}
		out = append(out, next)
	}
	return out, nil
}

func options(d data) (Options, bool) {
	if d.Options == nil {
		return DefaultOptions(len(d.Hands)), true
	}
	o := *d.Options
	return o, o.Seats == len(d.Hands) && o.Validate() == nil
}

func seatsValid(seats []int, n int) bool {
	for _, seat := range seats {
		if seat < 0 || seat >= n {
			return false
		}
	}
	return true
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
	n := len(d.Hands)
	if n < MinSeats || n > MaxSeats || n != len(state.Players) {
		return data{}, Position{}, malformed("seats")
	}
	var p Position
	var ok bool
	if p.Options, ok = options(d); !ok {
		return data{}, Position{}, malformed("options")
	}
	var err error
	p.Hands = make([][]Card, n)
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
	if p.Table, err = parseTable(d.Table, d.Attacker); err != nil {
		return data{}, Position{}, malformed("table")
	}
	if len(d.Places) != 0 && len(d.Places) != n || !seatsValid(d.Passed, n) {
		return data{}, Position{}, malformed("seats")
	}
	p.Attacker, p.Limit, p.Taking, p.Passed, p.Places = d.Attacker, d.Limit, d.Taking, d.Passed, d.Places
	for _, item := range p.Table {
		if item.By < 0 || item.By >= n {
			return data{}, Position{}, malformed("table")
		}
	}
	finished, _ := p.Outcome()
	seated := d.Attacker >= 0 && d.Attacker < n && (finished || p.active(d.Attacker) && p.Defender() >= 0)
	if !seated || d.Limit < 0 || d.Limit > HandSize || len(d.Table) > d.Limit {
		return data{}, Position{}, malformed("bout")
	}
	cards := p.Cards()
	slices.SortFunc(cards, byIdentity)
	if !slices.Equal(cards, DeckOf(p.Options.Deck)) {
		return data{}, Position{}, malformed("deck")
	}
	return d, p, nil
}

func (g Game) Init(players []engine.PlayerID) engine.State {
	return g.InitSeeded(players, 0)
}

func (g Game) InitSeeded(players []engine.PlayerID, seed uint64) engine.State {
	state, err := g.InitConfigured(players, seed, nil)
	if err != nil {
		state = engine.NewState(Key, players, nil)
		state.Seed = seed
		return state.Finish(engine.NoSeat)
	}
	return state
}

func (Game) ValidateOptions(raw []byte) error {
	_, err := ParseOptions(raw, 0)
	return err
}

func (Game) InitConfigured(players []engine.PlayerID, seed uint64, raw []byte) (engine.State, error) {
	if len(players) < MinSeats || len(players) > MaxSeats {
		return engine.State{}, fmt.Errorf("%w: durak seats %d..%d, got %d", engine.ErrPlayerCount, MinSeats, MaxSeats, len(players))
	}
	o, err := ParseOptions(raw, len(players))
	if err != nil {
		return engine.State{}, err
	}
	p := DealTable(seed, o)
	encoded, err := json.Marshal(encode(p, []string{}, Ongoing))
	if err != nil {
		return engine.State{}, fmt.Errorf("durak: encode state: %w", err)
	}
	state := engine.NewState(Key, players, encoded)
	state.Turn, state.Seed = p.Turn(), seed
	return state, nil
}

func precheck(state engine.State, move engine.Move, m Move, parsed bool) (int, error) {
	if !parsed || m.Action != Catch {
		return engine.Precheck(Key, state, move)
	}
	probe := state
	if seat, ok := state.Seat(move.Player); ok && !state.Over() {
		probe.Turn = seat
	}
	return engine.Precheck(Key, probe, move)
}

func (Game) ApplyMove(state engine.State, move engine.Move) (engine.State, error) {
	var in MoveData
	moveErr := engine.DecodeMove(move, &in)
	var m Move
	parseErr := moveErr
	if moveErr == nil {
		m, parseErr = ParseMove(in.Move)
	}
	seat, err := precheck(state, move, m, parseErr == nil)
	if err != nil {
		return engine.State{}, err
	}
	d, p, err := decode(state)
	if err != nil {
		return engine.State{}, err
	}
	if parseErr != nil {
		return engine.State{}, parseErr
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
	}
	raw, err := json.Marshal(encode(next, append(slices.Clone(d.Moves), m.String()), end))
	if err != nil {
		return engine.State{}, fmt.Errorf("durak: encode state: %w", err)
	}
	result := state.Advance(raw, next.Turn())
	if over {
		result = result.Finish(winner)
	}
	if len(next.Hands) > 2 && slices.ContainsFunc(next.Places, func(place int) bool { return place > 0 }) {
		result.Places = slices.Clone(next.Places)
	}
	return result, nil
}

func (Game) ValidMoves(state engine.State, player engine.PlayerID) []engine.Move {
	if state.Over() {
		return nil
	}
	seat, ok := state.Seat(player)
	if !ok {
		return nil
	}
	_, p, err := decode(state)
	if err != nil || p.Turn() != state.Turn {
		return nil
	}
	legal := p.LegalFor(seat)
	if len(legal) == 0 {
		return nil
	}
	moves := make([]engine.Move, 0, len(legal))
	for _, m := range legal {
		moves = append(moves, NewMove(player, state.Ply, m.String()))
	}
	return moves
}

func (Game) IsTerminal(state engine.State) (bool, *engine.PlayerID) {
	return state.Outcome()
}

func (Game) BotMove(state engine.State, player engine.PlayerID, seed uint64) (engine.Move, error) {
	probe := state
	seat, ok := state.Seat(player)
	if ok && !state.Over() {
		probe.Turn = seat
	}
	if _, err := engine.Precheck(Key, probe, engine.Move{Player: player, Ply: state.Ply}); err != nil {
		return engine.Move{}, err
	}
	_, p, err := decode(state)
	if err != nil {
		return engine.Move{}, err
	}
	m, ok := p.Bot(seat, seed)
	if !ok {
		return engine.Move{}, fmt.Errorf("%w: seat %d has nothing to do, seat %d is to move", engine.ErrOutOfTurn, seat, state.Turn)
	}
	return NewMove(player, state.Ply, m.String()), nil
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
	for i := range d.Table {
		d.Table[i].Cheat = nil
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
	By      int
	Checked []string
}

type Snapshot struct {
	Hands    []Hand
	Trump    string
	Stock    int
	Table    []TablePair
	Discard  int
	Attacker int
	Defender int
	Taking   bool
	Limit    int
	Passed   []int
	Places   []int
	Options  Options
	Moves    []string
	LastMove string
	Legal    []string
	End      End
}

func legacyPlaces(d data) []int {
	if d.Places != nil || d.End == Ongoing {
		return d.Places
	}
	places := make([]int, len(d.Hands))
	for seat, hand := range d.Hands {
		places[seat] = len(d.Hands)
		if len(hand) == 0 {
			places[seat] = 1
		}
	}
	return places
}

func Describe(state engine.State) (Snapshot, error) {
	var d data
	if err := engine.DecodeState(state, &d); err != nil {
		return Snapshot{}, err
	}
	malformed := func(what string) error { return fmt.Errorf("%w: %s", engine.ErrStateMalformed, what) }
	o, ok := options(d)
	if !ok || len(d.Held) != len(d.Hands) {
		return Snapshot{}, malformed("seats")
	}
	snap := Snapshot{
		Trump: d.Trump, Stock: d.Left, Discard: len(d.Discard), Attacker: d.Attacker, Defender: engine.NoSeat, Taking: d.Taking,
		Limit: d.Limit, Passed: d.Passed, Places: legacyPlaces(d), Options: o, Moves: d.Moves, End: d.End,
	}
	for seat := range d.Hands {
		visible := d.Viewer == nil || *d.Viewer == seat
		snap.Hands = append(snap.Hands, Hand{Count: d.Held[seat], Hidden: !visible})
		if visible {
			snap.Hands[seat].Cards = d.Hands[seat]
		}
	}
	p := Position{Options: o, Attacker: d.Attacker, Limit: d.Limit, Taking: d.Taking, Passed: d.Passed, Places: d.Places, Hands: make([][]Card, len(d.Hands))}
	var err error
	if p.Table, err = parseTable(d.Table, d.Attacker); err != nil {
		return Snapshot{}, malformed("table")
	}
	for _, item := range p.Table {
		next := TablePair{Attack: item.Attack.String(), By: item.By, Checked: marked(item, item.Checked)}
		if item.Covered {
			next.Defense = item.Defense.String()
		}
		snap.Table = append(snap.Table, next)
	}
	if n := len(d.Moves); n > 0 {
		snap.LastMove = d.Moves[n-1]
	}
	if len(d.Places) != 0 && len(d.Places) != len(d.Hands) {
		return Snapshot{}, malformed("seats")
	}
	for seat, held := range d.Held {
		if held < 0 || held > o.Deck {
			return Snapshot{}, malformed("hand")
		}
		p.Hands[seat] = make([]Card, held)
	}
	if !p.active(d.Attacker) {
		return snap, nil
	}
	snap.Defender = p.Defender()
	if state.Over() || d.Viewer == nil || *d.Viewer < 0 || *d.Viewer >= len(d.Hands) || state.Turn < 0 || state.Turn >= len(d.Hands) {
		return snap, nil
	}
	viewer := *d.Viewer
	if p.Trump, err = ParseCard(d.Trump); err != nil {
		return Snapshot{}, malformed("trump")
	}
	if p.Hands[viewer], err = parseCards(d.Hands[viewer]); err != nil {
		return Snapshot{}, malformed("hand")
	}
	if p.Discard, err = parseCards(d.Discard); err != nil {
		return Snapshot{}, malformed("discard")
	}
	if p.Turn() != state.Turn {
		return snap, nil
	}
	for _, m := range p.LegalFor(viewer) {
		snap.Legal = append(snap.Legal, m.String())
	}
	return snap, nil
}

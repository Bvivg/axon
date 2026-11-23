package durak

import (
	"fmt"
	"slices"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type Mark uint8

const (
	MarkAttack Mark = 1 << iota
	MarkDefense
)

type Pair struct {
	Attack  Card
	Defense Card
	Covered bool
	By      int
	Cheat   Mark
	Checked Mark
}

type Position struct {
	Options  Options
	Hands    [][]Card
	Stock    []Card
	Trump    Card
	Table    []Pair
	Discard  []Card
	Attacker int
	Limit    int
	Taking   bool
	Passed   []int
	Places   []int
}

func Deal(seed uint64) Position {
	return DealTable(seed, DefaultOptions(MinSeats))
}

func DealTable(seed uint64, o Options) Position {
	deck := ShuffledDeck(seed, o.Deck)
	dealt := o.Seats * HandSize
	p := Position{Options: o, Hands: make([][]Card, o.Seats)}
	for seat := range o.Seats {
		p.Hands[seat] = slices.Clone(deck[seat*HandSize : (seat+1)*HandSize])
	}
	if dealt < len(deck) {
		p.Trump = deck[dealt]
		p.Stock = append(slices.Clone(deck[dealt+1:]), p.Trump)
	} else {
		p.Trump = deck[dealt-1]
		p.Stock = []Card{}
	}
	for _, hand := range p.Hands {
		sortHand(hand, p.Trump.Suit)
	}
	p.Attacker = FirstAttacker(p.Hands, p.Trump.Suit)
	p.Limit = min(p.boutCap(), len(p.Hands[p.Defender()]))
	return p
}

func FirstAttacker(hands [][]Card, trump Suit) int {
	seat, lowest := 0, Ace+1
	for s, hand := range hands {
		for _, c := range hand {
			if c.Suit == trump && c.Rank < lowest {
				seat, lowest = s, c.Rank
			}
		}
	}
	return seat
}

func (p Position) active(seat int) bool {
	if seat < 0 || seat >= len(p.Hands) {
		return false
	}
	return seat >= len(p.Places) || p.Places[seat] == 0
}

func (p Position) nextActive(seat int) int {
	for i := 1; i < len(p.Hands); i++ {
		if next := (seat + i) % len(p.Hands); p.active(next) {
			return next
		}
	}
	return engine.NoSeat
}

func (p Position) Defender() int { return p.nextActive(p.Attacker) }

func (p Position) boutCap() int {
	if len(p.Discard) == 0 && p.Options.FirstBoutLimit > 0 {
		return min(HandSize, p.Options.FirstBoutLimit)
	}
	return HandSize
}

func (p Position) open() int {
	for i, pair := range p.Table {
		if !pair.Covered {
			return i
		}
	}
	return -1
}

func (p Position) uncovered(attack Card) int {
	return slices.IndexFunc(p.Table, func(pair Pair) bool { return !pair.Covered && pair.Attack == attack })
}

func (p Position) defending() bool {
	return len(p.Table) > 0 && !p.Taking && p.open() >= 0
}

func (p Position) throwers() []int {
	out := []int{p.Attacker}
	if len(p.Table) >= p.Limit {
		return out
	}
	defender := p.Defender()
	neighbor := p.nextActive(defender)
	for i := 1; i < len(p.Hands); i++ {
		seat := (p.Attacker + i) % len(p.Hands)
		if seat == defender || !p.active(seat) || len(p.Hands[seat]) == 0 {
			continue
		}
		if p.Options.ThrowIn == ThrowInNeighbors && seat != neighbor {
			continue
		}
		out = append(out, seat)
	}
	return out
}

func (p Position) Turn() int {
	if len(p.Table) == 0 {
		return p.Attacker
	}
	if p.defending() {
		return p.Defender()
	}
	for _, seat := range p.throwers() {
		if !slices.Contains(p.Passed, seat) {
			return seat
		}
	}
	return p.Attacker
}

func (p Position) onTable(rank Rank) bool {
	for _, pair := range p.Table {
		if pair.Attack.Rank == rank || pair.Covered && pair.Defense.Rank == rank {
			return true
		}
	}
	return false
}

func (p Position) canAttack(seat int, c Card) bool {
	if seat != p.Turn() || p.defending() || len(p.Table) >= p.Limit {
		return false
	}
	return len(p.Table) == 0 || p.onTable(c.Rank)
}

func (p Position) canTransfer(c Card) bool {
	if p.Options.Mode != ModeTransfer || !p.defending() {
		return false
	}
	for _, pair := range p.Table {
		if pair.Covered || pair.Attack.Rank != c.Rank {
			return false
		}
	}
	target := p.nextActive(p.Defender())
	return target >= 0 && len(p.Table) < min(p.boutCap(), len(p.Hands[target]))
}

func (p Position) CheatAllowed(seat int, m Move) bool {
	if !p.Options.Cheats || seat != p.Turn() || !slices.Contains(p.Hands[seat], m.Card) {
		return false
	}
	switch m.Action {
	case Attack:
		return len(p.Table) > 0 && !p.defending() && len(p.Table) < p.Limit && !p.onTable(m.Card.Rank)
	case Defend:
		return p.defending() && p.uncovered(m.Against) >= 0 && !Beats(m.Card, m.Against, p.Trump.Suit)
	default:
		return false
	}
}

func (p Position) Legal() []Move {
	seat := p.Turn()
	if seat < 0 || seat >= len(p.Hands) {
		return nil
	}
	return append(p.turnMoves(seat), p.catches(seat)...)
}

func (p Position) LegalFor(seat int) []Move {
	if seat == p.Turn() {
		return p.Legal()
	}
	return p.catches(seat)
}

func (p Position) turnMoves(seat int) []Move {
	var out []Move
	hand := p.Hands[seat]
	if p.defending() {
		for _, pair := range p.Table {
			if pair.Covered {
				continue
			}
			for _, c := range hand {
				if Beats(c, pair.Attack, p.Trump.Suit) {
					out = append(out, Move{Action: Defend, Card: c, Against: pair.Attack})
				}
			}
		}
		for _, c := range hand {
			if p.canTransfer(c) {
				out = append(out, Move{Action: Transfer, Card: c})
			}
		}
		return append(out, Move{Action: Take})
	}
	for _, c := range hand {
		if p.canAttack(seat, c) {
			out = append(out, Move{Action: Attack, Card: c})
		}
	}
	if len(p.Table) > 0 {
		out = append(out, Move{Action: Pass})
	}
	return out
}

func (p Position) catches(seat int) []Move {
	if !p.Options.Cheats || len(p.Table) == 0 || !p.active(seat) {
		return nil
	}
	defender := p.Defender()
	var out []Move
	for _, pair := range p.Table {
		if pair.By != seat && pair.Checked&MarkAttack == 0 {
			out = append(out, Move{Action: Catch, Card: pair.Attack})
		}
		if pair.Covered && seat != defender && pair.Checked&MarkDefense == 0 {
			out = append(out, Move{Action: Catch, Card: pair.Defense})
		}
	}
	return out
}

func (p Position) clone() Position {
	next := p
	next.Hands = make([][]Card, len(p.Hands))
	for seat, hand := range p.Hands {
		next.Hands[seat] = slices.Clone(hand)
	}
	next.Stock = slices.Clone(p.Stock)
	next.Table = slices.Clone(p.Table)
	next.Discard = slices.Clone(p.Discard)
	next.Passed = slices.Clone(p.Passed)
	next.Places = slices.Clone(p.Places)
	return next
}

func (p Position) Play(seat int, m Move) (Position, error) {
	if seat < 0 || seat >= len(p.Hands) {
		return Position{}, fmt.Errorf("%w: no seat %d", ErrIllegalMove, seat)
	}
	if m.Action == Catch {
		return p.catch(seat, m.Card)
	}
	if seat != p.Turn() {
		return Position{}, fmt.Errorf("%w: seat %d acted, seat %d is to act", ErrIllegalMove, seat, p.Turn())
	}
	illegal := fmt.Errorf("%w: %s", ErrIllegalMove, m)
	next := p.clone()
	at := slices.Index(next.Hands[seat], m.Card)
	switch m.Action {
	case Attack:
		var cheat Mark
		if !p.canAttack(seat, m.Card) {
			if !p.CheatAllowed(seat, m) {
				return Position{}, illegal
			}
			cheat = MarkAttack
		}
		if at < 0 {
			return Position{}, illegal
		}
		next.Hands[seat] = slices.Delete(next.Hands[seat], at, at+1)
		next.Table = append(next.Table, Pair{Attack: m.Card, By: seat, Cheat: cheat})
		next.Passed = nil
	case Defend:
		i := p.uncovered(m.Against)
		if !p.defending() || i < 0 || at < 0 {
			return Position{}, illegal
		}
		var cheat Mark
		if !Beats(m.Card, m.Against, p.Trump.Suit) {
			if !p.CheatAllowed(seat, m) {
				return Position{}, illegal
			}
			cheat = MarkDefense
		}
		next.Hands[seat] = slices.Delete(next.Hands[seat], at, at+1)
		next.Table[i].Defense, next.Table[i].Covered = m.Card, true
		next.Table[i].Cheat |= cheat
		next.Passed = nil
	case Transfer:
		if at < 0 || !p.canTransfer(m.Card) {
			return Position{}, illegal
		}
		next.Hands[seat] = slices.Delete(next.Hands[seat], at, at+1)
		next.Table = append(next.Table, Pair{Attack: m.Card, By: seat})
		next.Attacker = seat
		next.Limit = min(next.boutCap(), len(next.Hands[next.Defender()]))
		next.Passed = nil
	case Take:
		if !p.defending() {
			return Position{}, illegal
		}
		next.Taking = true
		next.Passed = nil
	case Pass:
		if len(p.Table) == 0 || p.defending() {
			return Position{}, illegal
		}
		next.Passed = append(next.Passed, seat)
		if next.allPassed() {
			receiver := engine.NoSeat
			if next.Taking {
				receiver = next.Defender()
			}
			next.endBout(receiver)
		}
	default:
		return Position{}, fmt.Errorf("%w: unknown action", ErrIllegalMove)
	}
	return next, nil
}

func (p Position) allPassed() bool {
	for _, seat := range p.throwers() {
		if !slices.Contains(p.Passed, seat) {
			return false
		}
	}
	return true
}

func (p Position) locate(c Card) (int, Mark) {
	for i, pair := range p.Table {
		switch {
		case pair.Attack == c:
			return i, MarkAttack
		case pair.Covered && pair.Defense == c:
			return i, MarkDefense
		}
	}
	return -1, 0
}

func (p Position) catch(seat int, c Card) (Position, error) {
	if !slices.Contains(p.catches(seat), Move{Action: Catch, Card: c}) {
		return Position{}, fmt.Errorf("%w: catch %s", ErrIllegalMove, c)
	}
	i, mark := p.locate(c)
	next := p.clone()
	if p.Table[i].Cheat&mark == 0 {
		next.Table[i].Checked |= mark
		return next, nil
	}
	cheater := p.Table[i].By
	if mark == MarkDefense {
		cheater = p.Defender()
	}
	next.endBout(cheater)
	return next, nil
}

func (p *Position) endBout(receiver int) {
	attacker, defender := p.Attacker, p.Defender()
	played := make([]Card, 0, 2*len(p.Table))
	for _, pair := range p.Table {
		played = append(played, pair.Attack)
		if pair.Covered {
			played = append(played, pair.Defense)
		}
	}
	if receiver >= 0 {
		p.Hands[receiver] = append(p.Hands[receiver], played...)
	} else {
		p.Discard = append(p.Discard, played...)
	}
	p.Table, p.Passed, p.Taking = nil, nil, false
	p.draw(attacker)
	for i := 1; i < len(p.Hands); i++ {
		if seat := (attacker + i) % len(p.Hands); seat != defender && p.active(seat) {
			p.draw(seat)
		}
	}
	p.draw(defender)
	p.place(defender)
	switch {
	case receiver == defender:
		if next := p.nextActive(defender); next >= 0 {
			p.Attacker = next
		}
	case p.active(defender):
		p.Attacker = defender
	default:
		if next := p.nextActive(defender); next >= 0 {
			p.Attacker = next
		} else {
			p.Attacker = defender
		}
	}
	p.Limit = 0
	if next := p.Defender(); next >= 0 {
		p.Limit = min(p.boutCap(), len(p.Hands[next]))
	}
}

func (p *Position) draw(seat int) {
	for len(p.Hands[seat]) < HandSize && len(p.Stock) > 0 {
		p.Hands[seat] = append(p.Hands[seat], p.Stock[0])
		p.Stock = p.Stock[1:]
	}
	sortHand(p.Hands[seat], p.Trump.Suit)
}

func (p *Position) place(defender int) {
	if len(p.Stock) > 0 {
		return
	}
	var out []int
	playing := 0
	for seat, hand := range p.Hands {
		if !p.active(seat) {
			continue
		}
		playing++
		if len(hand) == 0 {
			out = append(out, seat)
		}
	}
	if len(out) == 0 {
		return
	}
	if len(p.Places) != len(p.Hands) {
		p.Places = make([]int, len(p.Hands))
	}
	last := len(p.Hands)
	for _, seat := range out {
		p.Places[seat] = last - playing + 1
	}
	switch remaining := playing - len(out); {
	case remaining == 1:
		p.Places[slices.Index(p.Places, 0)] = last
	case remaining == 0 && !p.Options.Draw:
		p.Places[defender] = last
	}
}

func (p Position) Durak() int {
	if len(p.Places) != len(p.Hands) || slices.Contains(p.Places, 0) {
		return engine.NoSeat
	}
	return slices.Index(p.Places, len(p.Hands))
}

func (p Position) Outcome() (bool, int) {
	if len(p.Places) != len(p.Hands) || len(p.Hands) == 0 || slices.Contains(p.Places, 0) {
		return false, engine.NoSeat
	}
	if p.Durak() < 0 {
		return true, engine.NoSeat
	}
	return true, slices.Index(p.Places, 1)
}

func (p Position) Cards() []Card {
	out := make([]Card, 0, max(p.Options.Deck, DeckSize))
	for _, hand := range p.Hands {
		out = append(out, hand...)
	}
	out = append(out, p.Stock...)
	for _, pair := range p.Table {
		out = append(out, pair.Attack)
		if pair.Covered {
			out = append(out, pair.Defense)
		}
	}
	return append(out, p.Discard...)
}

package durak

import (
	"fmt"
	"slices"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type Pair struct {
	Attack  Card
	Defense Card
	Covered bool
}

type Position struct {
	Hands    [2][]Card
	Stock    []Card
	Trump    Card
	Table    []Pair
	Discard  []Card
	Attacker int
	Limit    int
	Taking   bool
}

func Deal(seed uint64) Position {
	deck := Shuffled(seed)
	trump := deck[2*HandSize]
	p := Position{Trump: trump}
	p.Hands[0] = slices.Clone(deck[:HandSize])
	p.Hands[1] = slices.Clone(deck[HandSize : 2*HandSize])
	p.Stock = append(slices.Clone(deck[2*HandSize+1:]), trump)
	sortHand(p.Hands[0], trump.Suit)
	sortHand(p.Hands[1], trump.Suit)
	p.Attacker = FirstAttacker(p.Hands, trump.Suit)
	p.Limit = min(HandSize, len(p.Hands[p.Defender()]))
	return p
}

func FirstAttacker(hands [2][]Card, trump Suit) int {
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

func (p Position) Defender() int { return 1 - p.Attacker }

func (p Position) open() int {
	for i, pair := range p.Table {
		if !pair.Covered {
			return i
		}
	}
	return -1
}

func (p Position) Turn() int {
	if !p.Taking && p.open() >= 0 {
		return p.Defender()
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

func (p Position) canAttack(c Card) bool {
	if p.Turn() != p.Attacker || len(p.Table) >= p.Limit {
		return false
	}
	return len(p.Table) == 0 || p.onTable(c.Rank)
}

func (p Position) Legal() []Move {
	seat := p.Turn()
	var out []Move
	if seat == p.Defender() {
		attack := p.Table[p.open()].Attack
		for _, c := range p.Hands[seat] {
			if Beats(c, attack, p.Trump.Suit) {
				out = append(out, Move{Action: Defend, Card: c, Against: attack})
			}
		}
		return append(out, Move{Action: Take})
	}
	for _, c := range p.Hands[seat] {
		if p.canAttack(c) {
			out = append(out, Move{Action: Attack, Card: c})
		}
	}
	if len(p.Table) > 0 {
		out = append(out, Move{Action: Pass})
	}
	return out
}

func (p Position) clone() Position {
	next := p
	next.Hands = [2][]Card{slices.Clone(p.Hands[0]), slices.Clone(p.Hands[1])}
	next.Stock = slices.Clone(p.Stock)
	next.Table = slices.Clone(p.Table)
	next.Discard = slices.Clone(p.Discard)
	return next
}

func (p Position) Play(seat int, m Move) (Position, error) {
	if seat != p.Turn() {
		return Position{}, fmt.Errorf("%w: seat %d acted, seat %d is to act", ErrIllegalMove, seat, p.Turn())
	}
	next := p.clone()
	switch m.Action {
	case Attack:
		at := slices.Index(next.Hands[seat], m.Card)
		if at < 0 || !p.canAttack(m.Card) {
			return Position{}, fmt.Errorf("%w: %s", ErrIllegalMove, m)
		}
		next.Hands[seat] = slices.Delete(next.Hands[seat], at, at+1)
		next.Table = append(next.Table, Pair{Attack: m.Card})
	case Defend:
		open := p.open()
		at := slices.Index(next.Hands[seat], m.Card)
		if seat != p.Defender() || open < 0 || p.Table[open].Attack != m.Against || at < 0 || !Beats(m.Card, m.Against, p.Trump.Suit) {
			return Position{}, fmt.Errorf("%w: %s", ErrIllegalMove, m)
		}
		next.Hands[seat] = slices.Delete(next.Hands[seat], at, at+1)
		next.Table[open].Defense, next.Table[open].Covered = m.Card, true
	case Take:
		if seat != p.Defender() {
			return Position{}, fmt.Errorf("%w: %s", ErrIllegalMove, m)
		}
		next.Taking = true
	case Pass:
		if seat != p.Attacker || len(p.Table) == 0 {
			return Position{}, fmt.Errorf("%w: %s", ErrIllegalMove, m)
		}
		next.endBout()
	default:
		return Position{}, fmt.Errorf("%w: unknown action", ErrIllegalMove)
	}
	return next, nil
}

func (p *Position) endBout() {
	attacker, defender := p.Attacker, p.Defender()
	played := make([]Card, 0, 2*len(p.Table))
	for _, pair := range p.Table {
		played = append(played, pair.Attack)
		if pair.Covered {
			played = append(played, pair.Defense)
		}
	}
	if p.Taking {
		p.Hands[defender] = append(p.Hands[defender], played...)
	} else {
		p.Discard = append(p.Discard, played...)
	}
	p.Table = nil
	p.draw(attacker)
	p.draw(defender)
	if !p.Taking {
		p.Attacker = defender
	}
	p.Taking = false
	p.Limit = min(HandSize, len(p.Hands[p.Defender()]))
}

func (p *Position) draw(seat int) {
	for len(p.Hands[seat]) < HandSize && len(p.Stock) > 0 {
		p.Hands[seat] = append(p.Hands[seat], p.Stock[0])
		p.Stock = p.Stock[1:]
	}
	sortHand(p.Hands[seat], p.Trump.Suit)
}

func (p Position) Outcome() (bool, int) {
	if len(p.Stock) > 0 || len(p.Table) > 0 {
		return false, engine.NoSeat
	}
	out0, out1 := len(p.Hands[0]) == 0, len(p.Hands[1]) == 0
	switch {
	case out0 && out1:
		return true, engine.NoSeat
	case out0:
		return true, 0
	case out1:
		return true, 1
	default:
		return false, engine.NoSeat
	}
}

func (p Position) Cards() []Card {
	out := make([]Card, 0, DeckSize)
	out = append(out, p.Hands[0]...)
	out = append(out, p.Hands[1]...)
	out = append(out, p.Stock...)
	for _, pair := range p.Table {
		out = append(out, pair.Attack)
		if pair.Covered {
			out = append(out, pair.Defense)
		}
	}
	return append(out, p.Discard...)
}

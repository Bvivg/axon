package durak

import "slices"

func (p Position) Bot(seat int, seed uint64) (Move, bool) {
	if seat < 0 || seat >= len(p.Hands) {
		return Move{}, false
	}
	if c, ok := p.exposed(seat); ok {
		return Move{Action: Catch, Card: c}, true
	}
	if seat != p.Turn() {
		return Move{}, false
	}
	if p.defending() {
		return p.botDefend(seat), true
	}
	return p.botAttack(seat, seed), true
}

func (p Position) exposed(seat int) (Card, bool) {
	for _, m := range p.catches(seat) {
		if p.dishonest(m.Card) {
			return m.Card, true
		}
	}
	return Card{}, false
}

func (p Position) dishonest(c Card) bool {
	var seen [Ace + 1]bool
	for i, pair := range p.Table {
		if pair.Attack == c {
			return i > 0 && !seen[c.Rank]
		}
		seen[pair.Attack.Rank] = true
		if !pair.Covered {
			continue
		}
		if pair.Defense == c {
			return !Beats(pair.Defense, pair.Attack, p.Trump.Suit)
		}
		seen[pair.Defense.Rank] = true
	}
	return false
}

func (p Position) cost(c Card) int {
	if c.Suit == p.Trump.Suit {
		return int(c.Rank) + 32
	}
	return int(c.Rank)
}

func (p Position) cover(hand []Card) ([]Card, bool) {
	plan := make([]Card, len(p.Table))
	var open []int
	for i, pair := range p.Table {
		if !pair.Covered {
			open = append(open, i)
		}
	}
	slices.SortStableFunc(open, func(a, b int) int { return p.cost(p.Table[b].Attack) - p.cost(p.Table[a].Attack) })
	used := make([]bool, len(hand))
	for _, i := range open {
		best := -1
		for j, c := range hand {
			if used[j] || !Beats(c, p.Table[i].Attack, p.Trump.Suit) {
				continue
			}
			if best < 0 || p.cost(c) < p.cost(hand[best]) {
				best = j
			}
		}
		if best < 0 {
			return nil, false
		}
		used[best] = true
		plan[i] = hand[best]
	}
	return plan, true
}

func (p Position) hoards(plan []Card) bool {
	if len(p.Stock) < HandSize || len(p.Table) > 2 {
		return false
	}
	return slices.ContainsFunc(plan, func(c Card) bool { return c.Suit == p.Trump.Suit && c.Rank >= Jack })
}

func (p Position) botDefend(seat int) Move {
	hand := p.Hands[seat]
	plan, covered := p.cover(hand)
	found := false
	var transfer Card
	for _, c := range hand {
		if p.canTransfer(c) && (!found || p.cost(c) < p.cost(transfer)) {
			transfer, found = c, true
		}
	}
	if found && (transfer.Suit != p.Trump.Suit || !covered) {
		return Move{Action: Transfer, Card: transfer}
	}
	if !covered || p.hoards(plan) {
		return Move{Action: Take}
	}
	for i, pair := range p.Table {
		if !pair.Covered {
			return Move{Action: Defend, Card: plan[i], Against: pair.Attack}
		}
	}
	return Move{Action: Take}
}

func (p Position) cheap(c Card) bool {
	return len(p.Stock) == 0 || c.Suit != p.Trump.Suit && c.Rank <= Jack
}

func (p Position) botAttack(seat int, seed uint64) Move {
	var options []Card
	for _, c := range p.Hands[seat] {
		if p.canAttack(seat, c) && (len(p.Table) == 0 || p.cheap(c)) {
			options = append(options, c)
		}
	}
	if len(options) == 0 {
		return Move{Action: Pass}
	}
	lowest := p.cost(options[0])
	for _, c := range options {
		lowest = min(lowest, p.cost(c))
	}
	ties := slices.DeleteFunc(options, func(c Card) bool { return p.cost(c) != lowest })
	return Move{Action: Attack, Card: ties[seed%uint64(len(ties))]}
}

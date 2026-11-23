package durak

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func seats(t *testing.T, o Options, attacker int, places []int, stock string, hands ...string) Position {
	t.Helper()
	o.Seats = len(hands)
	p := Position{Options: o, Trump: card(t, "AS"), Attacker: attacker, Stock: cards(t, stock), Places: places}
	for _, hand := range hands {
		p.Hands = append(p.Hands, cards(t, hand))
	}
	p.Limit = min(p.boutCap(), len(p.Hands[p.Defender()]))
	return p
}

func with(change func(*Options)) Options {
	o := DefaultOptions(0)
	change(&o)
	return o
}

func hand(p Position, seat int) string { return strings.Join(names(p.Hands[seat]), " ") }

func TestCircleOfSeats(t *testing.T) {
	cases := []struct {
		name     string
		hands    []string
		places   []int
		attacker int
		moves    []string
		turns    []int
		next     int
		defender int
	}{
		{"the next seat defends and attacks after a beaten bout", []string{"7H 9C", "8H 6D", "TC JC"}, nil, 0,
			[]string{"attack 7H", "defend 7H 8H", "pass", "pass"}, []int{1, 0, 2}, 1, 2},
		{"after a take the seat past the taker attacks", []string{"7H 9C", "6D 7D", "TC JC"}, nil, 0,
			[]string{"attack 7H", "take", "pass", "pass"}, []int{1, 0, 2}, 2, 0},
		{"the circle wraps around", []string{"9H TC", "7C 9C", "8H 6D"}, nil, 2,
			[]string{"attack 8H", "defend 8H 9H", "pass", "pass"}, []int{0, 2, 1}, 0, 1},
		{"seats that went out are skipped", []string{"", "7H 9C", "QC KC", "8H 6D"}, []int{1, 0, 0, 0}, 3,
			[]string{"attack 8H", "take", "pass", "pass"}, []int{1, 3, 2}, 2, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := seats(t, DefaultOptions(0), c.attacker, c.places, "", c.hands...)
			for i, raw := range c.moves {
				p = play(t, p, raw)
				if i < len(c.turns) && p.Turn() != c.turns[i] {
					t.Fatalf("after %s: turn %d, want %d", raw, p.Turn(), c.turns[i])
				}
			}
			if p.Attacker != c.next || p.Defender() != c.defender || p.Turn() != c.next {
				t.Fatalf("next bout: attacker %d defender %d turn %d, want %d/%d", p.Attacker, p.Defender(), p.Turn(), c.next, c.defender)
			}
		})
	}
}

func TestThrowIn(t *testing.T) {
	hands := []string{"7H 9D", "8H TC JH", "7C QC", "8C KC"}
	cases := []struct {
		name  string
		mode  ThrowIn
		moves []string
		turn  int
		legal []string
	}{
		{"everyone but the defender throws in", ThrowInAll, []string{"attack 7H", "defend 7H 8H", "pass"}, 2, []string{"attack 7C", "pass"}},
		{"the far seat throws in last", ThrowInAll, []string{"attack 7H", "defend 7H 8H", "pass", "pass"}, 3, []string{"attack 8C", "pass"}},
		{"a throw-in reopens the round", ThrowInAll, []string{"attack 7H", "defend 7H 8H", "pass", "attack 7C", "defend 7C TC"}, 0, []string{"pass"}},
		{"the bout ends when everyone passed", ThrowInAll, []string{"attack 7H", "defend 7H 8H", "pass", "pass", "pass"}, 1, []string{"attack TC", "attack JH"}},
		{"only the defender's neighbours throw in", ThrowInNeighbors, []string{"attack 7H", "defend 7H 8H", "pass"}, 2, []string{"attack 7C", "pass"}},
		{"the far seat sits out with neighbours", ThrowInNeighbors, []string{"attack 7H", "defend 7H 8H", "pass", "pass"}, 1, []string{"attack TC", "attack JH"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, seats(t, with(func(o *Options) { o.ThrowIn = c.mode }), 0, nil, "", hands...), c.moves...)
			if p.Turn() != c.turn {
				t.Fatalf("turn %d, want %d", p.Turn(), c.turn)
			}
			if got := legal(p); !slices.Equal(got, c.legal) {
				t.Fatalf("legal %v, want %v", got, c.legal)
			}
		})
	}
	far := play(t, seats(t, with(func(o *Options) { o.ThrowIn = ThrowInNeighbors }), 0, nil, "", hands...), "attack 7H", "defend 7H 8H", "pass")
	if _, err := far.Play(3, move(t, "attack 8C")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("a seat that is not a neighbour threw in", err)
	}
	full := play(t, seats(t, DefaultOptions(0), 0, nil, "", "7H 7D", "8H", "7C"), "attack 7H", "defend 7H 8H")
	if full.Turn() != 0 || !slices.Equal(legal(full), []string{"pass"}) {
		t.Fatal("a full table is closed for everyone", full.Turn(), legal(full))
	}
	if full = play(t, full, "pass"); len(full.Table) != 0 {
		t.Fatal("other seats were asked to pass at a full table")
	}
	empty := play(t, seats(t, DefaultOptions(0), 0, nil, "QS", "7H 9C", "8H TH", "", "7C"), "attack 7H", "defend 7H 8H", "pass")
	if empty.Turn() != 3 {
		t.Fatal("a seat without cards is not asked to throw in", empty.Turn())
	}
}

func TestTransfer(t *testing.T) {
	transfer := with(func(o *Options) { o.Mode = ModeTransfer })
	cases := []struct {
		name  string
		o     Options
		hands []string
		moves []string
		want  bool
	}{
		{"same rank passes the attack on", transfer, []string{"7H 9C", "7C 8D", "QC KC AC"}, []string{"attack 7H"}, true},
		{"exactly enough cards is enough", transfer, []string{"7H 9C", "7C 8D", "QC KC"}, []string{"attack 7H"}, true},
		{"throw-in mode has no transfer", DefaultOptions(0), []string{"7H 9C", "7C 8D", "QC KC"}, []string{"attack 7H"}, false},
		{"another rank cannot transfer", transfer, []string{"7H 9C", "8C 8D", "QC KC"}, []string{"attack 7H"}, false},
		{"the next defender must hold enough cards", transfer, []string{"7H 9C", "7C 8D", "QC"}, []string{"attack 7H"}, false},
		{"no transfer once a card is beaten", transfer, []string{"7H 7D 9C", "8H 7C 8D", "QC KC QH"}, []string{"attack 7H", "defend 7H 8H", "attack 7D"}, false},
		{"no transfer after taking", transfer, []string{"7H 7D", "7C 8D", "QC KC"}, []string{"attack 7H", "take"}, false},
		{"two seats transfer back to the attacker", transfer, []string{"7H 9C TC", "7C 8D"}, []string{"attack 7H"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, seats(t, c.o, 0, nil, "", c.hands...), c.moves...)
			offered := slices.Contains(legal(p), "transfer 7C")
			_, err := p.Play(p.Turn(), move(t, "transfer 7C"))
			if offered != c.want || (err == nil) != c.want {
				t.Fatalf("transfer offered %v, played err %v, want %v", offered, err, c.want)
			}
		})
	}
	p := play(t, seats(t, transfer, 0, nil, "", "7H 9C", "7C 8D", "7S QC KC"), "attack 7H", "transfer 7C")
	if p.Attacker != 1 || p.Defender() != 2 || p.Turn() != 2 || len(p.Table) != 2 || p.Table[1].By != 1 || p.Limit != 3 {
		t.Fatalf("after a transfer: attacker %d defender %d turn %d limit %d table %v", p.Attacker, p.Defender(), p.Turn(), p.Limit, p.Table)
	}
	if got := legal(p); !slices.Contains(got, "defend 7C KC") || !slices.Contains(got, "defend 7H 7S") || slices.Contains(got, "transfer 7S") {
		t.Fatal("the new defender beats either card and cannot pass three on to one card", got)
	}
	p = play(t, p, "defend 7C KC", "defend 7H 7S", "pass", "pass")
	if p.Attacker != 2 || hand(p, 2) != "QC" || len(p.Discard) != 4 {
		t.Fatalf("the defender of the transferred bout attacks next: attacker %d hand %s", p.Attacker, hand(p, 2))
	}
}

func TestRefillOrderAroundTheTable(t *testing.T) {
	p := seats(t, DefaultOptions(0), 1, nil, "6C 6D 6H",
		"7D TC TD TH 9C 9D", "7H AC AD AH KC KD", "8H 8S 9S TS JS QS", "7C QC QD QH JC JD")
	p = play(t, p, "attack 7H", "defend 7H 8H", "pass", "attack 7C", "defend 7C 8S", "pass", "pass", "attack 7D", "defend 7D 9S", "pass", "pass", "pass")
	if len(p.Table) != 0 || len(p.Discard) != 6 || len(p.Stock) != 0 || p.Attacker != 2 {
		t.Fatalf("bout did not end cleanly: %+v", p)
	}
	first, second, third := slices.Contains(p.Hands[1], card(t, "6C")), slices.Contains(p.Hands[3], card(t, "6D")), slices.Contains(p.Hands[0], card(t, "6H"))
	if !first || !second || !third || len(p.Hands[2]) != 3 {
		t.Fatalf("the attacker draws first, the throwers next around the table and the defender last: %s | %s | %s | %s", hand(p, 0), hand(p, 1), hand(p, 2), hand(p, 3))
	}
}

func TestPlacesAndDraw(t *testing.T) {
	cases := []struct {
		name   string
		draw   bool
		hands  []string
		places []int
		att    int
		moves  []string
		want   []int
		over   bool
		winner int
	}{
		{"two seats out together while one plays on", true, []string{"7H", "8H", "9C TC"}, nil, 0,
			[]string{"attack 7H", "defend 7H 8H", "pass"}, []int{1, 1, 3}, true, 0},
		{"one seat out, the game goes on", true, []string{"7H", "8H 9D", "9C TC"}, nil, 0,
			[]string{"attack 7H", "defend 7H 8H", "pass", "pass"}, []int{1, 0, 0}, false, engine.NoSeat},
		{"the last seat with cards is the durak", true, []string{"", "7H", "8H 9C"}, []int{1, 0, 0}, 1,
			[]string{"attack 7H", "defend 7H 8H", "pass"}, []int{1, 2, 3}, true, 0},
		{"the last two out together draw", true, []string{"", "7H", "8H"}, []int{1, 0, 0}, 1,
			[]string{"attack 7H", "defend 7H 8H", "pass"}, []int{1, 2, 2}, true, engine.NoSeat},
		{"without draws the last defender is the durak", false, []string{"", "7H", "8H"}, []int{1, 0, 0}, 1,
			[]string{"attack 7H", "defend 7H 8H", "pass"}, []int{1, 2, 3}, true, 0},
		{"two seats without draws", false, []string{"7H", "8H"}, nil, 0,
			[]string{"attack 7H", "defend 7H 8H", "pass"}, []int{1, 2}, true, 0},
		{"a seat that went out earlier keeps its place", true, []string{"", "7H 7D", "8H 8D", "9C"}, []int{1, 0, 0, 0}, 1,
			[]string{"attack 7H", "defend 7H 8H", "attack 7D", "defend 7D 8D", "pass"}, []int{1, 2, 2, 4}, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, seats(t, with(func(o *Options) { o.Draw = c.draw }), c.att, c.places, "", c.hands...), c.moves...)
			if !slices.Equal(p.Places, c.want) {
				t.Fatalf("places %v, want %v", p.Places, c.want)
			}
			over, winner := p.Outcome()
			if over != c.over || winner != c.winner {
				t.Fatalf("outcome %v/%d, want %v/%d", over, winner, c.over, c.winner)
			}
			if durak := p.Durak(); c.over && (durak == engine.NoSeat) != (winner == engine.NoSeat) {
				t.Fatalf("durak %d with winner %d", durak, winner)
			}
		})
	}
}

func TestFirstBoutLimit(t *testing.T) {
	five := with(func(o *Options) { o.FirstBoutLimit = 5 })
	if p := DealTable(3, with(func(o *Options) { o.FirstBoutLimit, o.Seats = 5, 2 })); p.Limit != 5 {
		t.Fatalf("a fresh deal with the first bout limited to five allows %d", p.Limit)
	}
	attacks, defence := "6H 6C 6D 7H 7C 7D", "7S 8C 8D 9H 9C 9D"
	bout := []string{"attack 6H", "defend 6H 7S", "attack 6C", "defend 6C 8C", "attack 6D", "defend 6D 8D", "attack 7H", "defend 7H 9H", "attack 7C", "defend 7C 9C"}
	if got := legal(play(t, seats(t, five, 0, nil, "", attacks, defence), bout...)); !slices.Equal(got, []string{"pass"}) {
		t.Fatal("a sixth card in the first bout", got)
	}
	if got := legal(play(t, seats(t, DefaultOptions(0), 0, nil, "", attacks, defence), bout...)); !slices.Equal(got, []string{"attack 7D", "pass"}) {
		t.Fatal("the default first bout takes six", got)
	}
	taken := play(t, seats(t, five, 0, nil, "", "6H 7H", "6C 6D 6S 7S 8S QS"), "attack 6H", "take", "pass")
	if taken.Limit != 5 {
		t.Fatalf("the limit lasts until the first beaten bout, got %d", taken.Limit)
	}
	beaten := play(t, seats(t, five, 0, nil, "AC AD", "6H 7H 8H 9H TH JH", "7S 8S 9S TS JS QS"), "attack 6H", "defend 6H 7S", "pass")
	if beaten.Limit != HandSize {
		t.Fatalf("after the first discard bouts take six, got %d", beaten.Limit)
	}
}

func TestDecks(t *testing.T) {
	cases := []struct {
		deck   int
		seats  int
		lowest Rank
		stock  int
	}{
		{24, 2, Nine, 12},
		{24, 4, Nine, 0},
		{36, 2, Six, 24},
		{36, 5, Six, 6},
		{36, 6, Six, 0},
		{52, 2, Two, 40},
		{52, 6, Two, 16},
	}
	for _, c := range cases {
		o := with(func(o *Options) { o.Deck, o.Seats = c.deck, c.seats })
		for _, seed := range []uint64{0, 9, 1 << 40} {
			p := DealTable(seed, o)
			all := p.Cards()
			slices.SortFunc(all, byIdentity)
			if !slices.Equal(all, DeckOf(c.deck)) || len(p.Stock) != c.stock || len(p.Hands) != c.seats {
				t.Fatalf("deck %d seats %d seed %d: %d cards, stock %d", c.deck, c.seats, seed, len(all), len(p.Stock))
			}
			if slices.ContainsFunc(all, func(card Card) bool { return card.Rank < c.lowest }) {
				t.Fatalf("deck %d dealt a card below %d", c.deck, c.lowest)
			}
			switch {
			case c.stock > 0 && p.Stock[len(p.Stock)-1] != p.Trump:
				t.Fatalf("deck %d seats %d: the trump %s is not under the stock", c.deck, c.seats, p.Trump)
			case c.stock == 0 && !slices.Contains(p.Hands[c.seats-1], p.Trump):
				t.Fatalf("deck %d seats %d: the trump %s is not the last dealt card", c.deck, c.seats, p.Trump)
			}
			if p.Limit != HandSize || p.Turn() != p.Attacker || p.Defender() != (p.Attacker+1)%c.seats {
				t.Fatalf("deck %d seats %d: bout %+v", c.deck, c.seats, p)
			}
		}
	}
	if len(DeckOf(40)) != 0 {
		t.Fatal("a 40-card deck exists")
	}
}

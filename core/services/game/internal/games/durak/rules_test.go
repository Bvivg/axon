package durak

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func cards(t *testing.T, raw string) []Card {
	t.Helper()
	if raw == "" {
		return []Card{}
	}
	out, err := parseCards(strings.Fields(raw))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func card(t *testing.T, raw string) Card {
	t.Helper()
	c, err := ParseCard(raw)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func move(t *testing.T, raw string) Move {
	t.Helper()
	m, err := ParseMove(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func play(t *testing.T, p Position, moves ...string) Position {
	t.Helper()
	for _, raw := range moves {
		next, err := p.Play(p.Turn(), move(t, raw))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		p = next
	}
	return p
}

func legal(p Position) []string {
	var out []string
	for _, m := range p.Legal() {
		out = append(out, m.String())
	}
	return out
}

func TestDealFromFixedSeed(t *testing.T) {
	for _, seed := range []uint64{0, 1, 42, 1 << 63} {
		p := Deal(seed)
		again := Deal(seed)
		if !slices.Equal(p.Cards(), again.Cards()) || p.Attacker != again.Attacker {
			t.Fatalf("seed %d dealt two different games", seed)
		}
		if len(p.Hands[0]) != HandSize || len(p.Hands[1]) != HandSize || len(p.Stock) != DeckSize-2*HandSize {
			t.Fatalf("seed %d: hands %d/%d, stock %d", seed, len(p.Hands[0]), len(p.Hands[1]), len(p.Stock))
		}
		if p.Stock[len(p.Stock)-1] != p.Trump {
			t.Fatalf("seed %d: the trump %s is not at the bottom of the stock", seed, p.Trump)
		}
		all := p.Cards()
		slices.SortFunc(all, func(a, b Card) int { return int(a.Suit)*16 + int(a.Rank) - int(b.Suit)*16 - int(b.Rank) })
		if !slices.Equal(all, Deck()) {
			t.Fatalf("seed %d did not deal the 36-card deck", seed)
		}
		if len(p.Table) != 0 || len(p.Discard) != 0 || p.Limit != HandSize || p.Turn() != p.Attacker {
			t.Fatalf("seed %d: bout did not start clean: %+v", seed, p)
		}
	}
	if slices.Equal(Deal(1).Cards(), Deal(2).Cards()) {
		t.Fatal("different seeds dealt the same order")
	}
	golden := Deal(7)
	if got := strings.Join(names(golden.Hands[0]), " ") + " | " + strings.Join(names(golden.Hands[1]), " ") + " | " + golden.Trump.String(); got != goldenDeal {
		t.Fatalf("the shuffle changed, replays of stored games would diverge:\n got %s\nwant %s", got, goldenDeal)
	}
}

const goldenDeal = "6S 6C 8C 9C AH KD | 8S QC AC 6H 6D TD | JD"

func TestTrumpAndFirstAttacker(t *testing.T) {
	cases := []struct {
		name  string
		hands [2]string
		trump Suit
		want  int
	}{
		{"seat 1 holds the only trump", [2]string{"6S 7S 8S", "9H 6C 7C"}, Hearts, 1},
		{"seat 0 holds the only trump", [2]string{"AH 7S 8S", "9C 6C 7C"}, Hearts, 0},
		{"lowest of two trumps", [2]string{"7D 8D", "6D AD"}, Diamonds, 1},
		{"lowest trump beats a lower plain card", [2]string{"6S 9C", "TC 7S"}, Clubs, 0},
		{"nobody holds a trump", [2]string{"6S 7S", "8S 9S"}, Hearts, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FirstAttacker([][]Card{cards(t, c.hands[0]), cards(t, c.hands[1])}, c.trump); got != c.want {
				t.Fatalf("first attacker = %d, want %d", got, c.want)
			}
		})
	}
	for seed := range uint64(50) {
		p := Deal(seed)
		lowest := Ace + 1
		holder := 0
		for seat, hand := range p.Hands {
			for _, c := range hand {
				if c.Suit == p.Trump.Suit && c.Rank < lowest {
					lowest, holder = c.Rank, seat
				}
			}
		}
		if p.Attacker != holder {
			t.Fatalf("seed %d: attacker %d, lowest trump with %d", seed, p.Attacker, holder)
		}
	}
}

func TestBeating(t *testing.T) {
	cases := []struct {
		defense, attack string
		want            bool
	}{
		{"KH", "7H", true},
		{"7H", "KH", false},
		{"7H", "7H", false},
		{"6S", "AH", true},
		{"AH", "6S", false},
		{"7S", "6S", true},
		{"6S", "7S", false},
		{"AC", "6D", false},
		{"6C", "AD", false},
	}
	for _, c := range cases {
		if got := Beats(card(t, c.defense), card(t, c.attack), Spades); got != c.want {
			t.Errorf("%s beats %s with spades trump = %v, want %v", c.defense, c.attack, got, c.want)
		}
	}
}

func bout(t *testing.T, attacker, defender, stock string, limit int) Position {
	t.Helper()
	p := Position{Options: DefaultOptions(2), Trump: card(t, "6S"), Attacker: 0, Limit: limit}
	p.Hands, p.Stock = [][]Card{cards(t, attacker), cards(t, defender)}, cards(t, stock)
	return p
}

func TestAttackAndDefenceMoves(t *testing.T) {
	p := bout(t, "7H 9C", "8H AH 6C 7S", "", 4)
	if got := legal(p); !slices.Equal(got, []string{"attack 7H", "attack 9C"}) {
		t.Fatal("opening attacks", got)
	}
	p = play(t, p, "attack 7H")
	if p.Turn() != 1 {
		t.Fatal("the defender must answer an attack")
	}
	if got := legal(p); !slices.Equal(got, []string{"defend 7H 8H", "defend 7H AH", "defend 7H 7S", "take"}) {
		t.Fatal("defences", got)
	}
	if _, err := p.Play(1, move(t, "defend 7H 6C")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("a lower plain card beat the attack", err)
	}
	if _, err := p.Play(1, move(t, "defend 9C AH")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("defended a card that is not on the table", err)
	}
	if _, err := p.Play(0, move(t, "attack 9C")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("the attacker acted out of turn", err)
	}
	p = play(t, p, "defend 7H 8H")
	if p.Turn() != 0 {
		t.Fatal("the attacker acts once every card is covered")
	}
	if got := legal(p); !slices.Equal(got, []string{"pass"}) {
		t.Fatal("9C matches no rank on the table", got)
	}
}

func TestThrowInLimits(t *testing.T) {
	cases := []struct {
		name     string
		attacker string
		defender string
		limit    int
		moves    []string
		want     []string
	}{
		{"rank on the table", "7H 7C 8D", "8H 9H TH", 3, []string{"attack 7H", "defend 7H 8H"}, []string{"attack 7C", "attack 8D", "pass"}},
		{"defence ranks count too", "8C 9D", "9C 8H", 2, []string{"attack 8C", "defend 8C 9C"}, []string{"attack 9D", "pass"}},
		{"never more than the defender held", "7H 7C 7D", "8H 8C", 2, []string{"attack 7H", "defend 7H 8H", "attack 7C", "defend 7C 8C"}, []string{"pass"}},
		{"six per bout", "6H 6C 6D 6S 7H 7C 7D", "7S 8H 8C 8D 9S TS AS", 6,
			[]string{"attack 6H", "defend 6H 8H", "attack 6C", "defend 6C 8C", "attack 6D", "defend 6D 8D", "attack 6S", "defend 6S 7S", "attack 7H", "defend 7H 9S", "attack 7C", "defend 7C TS"},
			[]string{"pass"}},
		{"throw-ins while the defender takes", "7H 7C 7D", "8S 9S", 2, []string{"attack 7H", "take"}, []string{"attack 7C", "attack 7D", "pass"}},
		{"throw-ins on a take stop at the limit", "7H 7C 7D", "8S 9S", 2, []string{"attack 7H", "take", "attack 7C"}, []string{"pass"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, bout(t, c.attacker, c.defender, "", c.limit), c.moves...)
			if got := legal(p); !slices.Equal(got, c.want) {
				t.Fatalf("legal = %v, want %v", got, c.want)
			}
			if last := p.Legal()[0]; last.Action == Attack && len(p.Table) >= p.Limit {
				t.Fatal("an attack was offered past the limit")
			}
		})
	}
	full := play(t, bout(t, "7H 7C 7D", "8H 8C", "", 2), "attack 7H", "defend 7H 8H", "attack 7C", "defend 7C 8C")
	if _, err := full.Play(0, move(t, "attack 7D")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("threw in past the limit", err)
	}
	if _, err := play(t, bout(t, "7H 9C", "8H", "", 1), "attack 7H", "defend 7H 8H").Play(0, move(t, "attack 9C")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("threw in a rank that is not on the table", err)
	}
}

func TestTakeAndPass(t *testing.T) {
	pass := play(t, bout(t, "7H 9C", "8H AH", "6C 6D 6H TC TD JD", 2), "attack 7H", "defend 7H 8H", "pass")
	if len(pass.Discard) != 2 || len(pass.Table) != 0 || pass.Attacker != 1 || pass.Turn() != 1 {
		t.Fatalf("after a pass the cards are discarded and the defender attacks: %+v", pass)
	}
	if got := strings.Join(names(pass.Hands[0]), " ") + " | " + strings.Join(names(pass.Hands[1]), " "); got != "6C 9C TC 6D TD 6H | JD AH" {
		t.Fatalf("refill after a pass: %s", got)
	}
	take := play(t, bout(t, "7H 7C 9D", "8S AH", "6C 6D", 2), "attack 7H", "take", "attack 7C", "pass")
	if len(take.Discard) != 0 || take.Attacker != 0 || take.Turn() != 0 || take.Taking {
		t.Fatalf("after a take the same attacker attacks again: %+v", take)
	}
	if got := strings.Join(names(take.Hands[1]), " "); got != "7C 7H AH 8S" {
		t.Fatalf("the defender picks up the table: %s", got)
	}
	if got := strings.Join(names(take.Hands[0]), " "); got != "6C 6D 9D" || len(take.Stock) != 0 {
		t.Fatalf("the attacker refills first: %s, stock %v", got, take.Stock)
	}
	if take.Limit != 4 {
		t.Fatalf("the next bout is limited by the defender's hand: %d", take.Limit)
	}
	covered := play(t, bout(t, "7H 7C", "8H 8C AS", "", 3), "attack 7H", "defend 7H 8H", "attack 7C", "take", "pass")
	if got := strings.Join(names(covered.Hands[1]), " "); got != "7C 8C 7H 8H AS" {
		t.Fatalf("taking picks up covered pairs too: %s", got)
	}
	if _, err := bout(t, "7H", "8H", "", 1).Play(0, move(t, "pass")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("passed on an empty table", err)
	}
	if _, err := play(t, bout(t, "7H", "8H", "", 1), "attack 7H").Play(1, move(t, "pass")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("the defender passed", err)
	}
	if _, err := play(t, bout(t, "7H 7C", "8H", "", 1), "attack 7H", "take").Play(1, move(t, "defend 7H 8H")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("defended after taking", err)
	}
}

func TestRefillOrder(t *testing.T) {
	cases := []struct {
		name         string
		stock        string
		wantAttacker string
		wantDefender string
		wantStock    int
	}{
		{"both refill to six", "6C 6D 6H 9H TH JH QH KH AH", "6C 7C 6D 7D 6H 9H", "TH JH QH KH QS KS", 1},
		{"the defender gets what is left", "6C 6D 6H 9H TH JH", "6C 7C 6D 7D 6H 9H", "TH JH QS KS", 0},
		{"the attacker drains the stock", "6C 6D", "6C 7C 6D 7D", "QS KS", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := bout(t, "7H 7C 7D 7S 8C 8D", "8H 9S TS JS QS KS", c.stock, 6)
			p.Trump = card(t, "AS")
			p = play(t, p, "attack 7H", "defend 7H 8H", "attack 7S", "defend 7S 9S", "attack 8C", "defend 8C TS", "attack 8D", "defend 8D JS", "pass")
			got := strings.Join(names(p.Hands[0]), " ") + " | " + strings.Join(names(p.Hands[1]), " ")
			if want := c.wantAttacker + " | " + c.wantDefender; got != want {
				t.Fatalf("hands %s, want %s", got, want)
			}
			if len(p.Stock) != c.wantStock || p.Attacker != 1 {
				t.Fatalf("stock %d next attacker %d", len(p.Stock), p.Attacker)
			}
		})
	}
}

func TestEndAndDraw(t *testing.T) {
	cases := []struct {
		name     string
		attacker string
		defender string
		moves    []string
		over     bool
		winner   int
	}{
		{"both empty in the same bout", "7H", "8H", []string{"attack 7H", "defend 7H 8H", "pass"}, true, engine.NoSeat},
		{"the attacker goes out on a take", "7H", "6C", []string{"attack 7H", "take", "pass"}, true, 0},
		{"the defender goes out", "7H 9C", "8H", []string{"attack 7H", "defend 7H 8H", "pass"}, true, 1},
		{"cards on both sides play on", "7H 9C", "8H 6D", []string{"attack 7H", "defend 7H 8H", "pass"}, false, engine.NoSeat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, bout(t, c.attacker, c.defender, "", min(HandSize, len(strings.Fields(c.defender)))), c.moves...)
			over, winner := p.Outcome()
			if over != c.over || winner != c.winner {
				t.Fatalf("outcome = %v/%d, want %v/%d", over, winner, c.over, c.winner)
			}
		})
	}
	mid := play(t, bout(t, "7H", "8H", "", 1), "attack 7H", "defend 7H 8H")
	if over, _ := mid.Outcome(); over {
		t.Fatal("a game ended in the middle of a bout")
	}
	stocked := Position{Hands: [][]Card{{}, cards(t, "8H")}, Stock: cards(t, "9S"), Trump: card(t, "9S")}
	if over, _ := stocked.Outcome(); over {
		t.Fatal("a game ended with cards in the stock")
	}
}

func TestParseMove(t *testing.T) {
	valid := []string{"attack 7H", "attack TS", "attack 2C", "attack 5H", "defend 7H KH", "defend 6D 6S", "take", "pass", "transfer 7C", "catch 9H"}
	for _, raw := range valid {
		m, err := ParseMove(raw)
		if err != nil || m.String() != raw {
			t.Errorf("ParseMove(%q) = %v, %v", raw, m, err)
		}
	}
	invalid := []string{"", " ", "attack", "attack 7H ", "attack 10H", "attack 1H", "attack 7X", "attack 7h", "defend 7H", "defend 7H KH 9C", "take now", "PASS", "beat 7H KH", "attack\t7H", "defend 7H  KH", strings.Repeat("a", 1<<16), "attack 7♥", "transfer", "transfer 7C 8C", "catch", "catch 7H 8H", "transfer 1H"}
	for _, raw := range invalid {
		if _, err := ParseMove(raw); !errors.Is(err, ErrBadMove) {
			t.Errorf("ParseMove(%q) = %v, want ErrBadMove", raw, err)
		}
	}
}

func FuzzParseMove(f *testing.F) {
	for _, seed := range []string{"attack 7H", "defend 7H KH", "take", "pass", "transfer 7C", "catch 9H", "", "defend 6S 6S6"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		m, err := ParseMove(raw)
		if err == nil && m.String() != raw {
			t.Fatalf("%q parsed into %q", raw, m.String())
		}
	})
}

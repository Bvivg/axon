package durak

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func TestBotChoices(t *testing.T) {
	transfer := with(func(o *Options) { o.Mode = ModeTransfer })
	cases := []struct {
		name  string
		p     func(t *testing.T) Position
		seat  int
		want  string
		quiet bool
	}{
		{"leads the lowest plain card", func(t *testing.T) Position {
			return seats(t, DefaultOptions(0), 0, nil, "", "6S KH 9D 7C", "8H 9H")
		}, 0, "attack 7C", false},
		{"leads a trump only when it has nothing else", func(t *testing.T) Position {
			return seats(t, DefaultOptions(0), 0, nil, "", "9S 7S", "8H 9H")
		}, 0, "attack 7S", false},
		{"beats with the cheapest card of the suit", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "", "7H 9D", "6S KH 9H 8C"), "attack 7H")
		}, 1, "defend 7H 9H", false},
		{"beats with a low trump when the suit is missing", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "", "7H 9D", "AS 6S 8C"), "attack 7H")
		}, 1, "defend 7H 6S", false},
		{"takes what it cannot beat", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "", "AH 9D", "6H 8C"), "attack AH")
		}, 1, "take", false},
		{"keeps a high trump early in the game", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "6C 7C 8C 9C TC JC", "AH 9D", "KS 8C"), "attack AH")
		}, 1, "take", false},
		{"spends a high trump once the stock is gone", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "", "AH 9D", "KS 8C"), "attack AH")
		}, 1, "defend AH KS", false},
		{"transfers with a plain card", func(t *testing.T) Position {
			return play(t, seats(t, transfer, 0, nil, "", "7H 9D", "7C 8H", "QC KC AC"), "attack 7H")
		}, 1, "transfer 7C", false},
		{"keeps a trump instead of transferring when it can beat", func(t *testing.T) Position {
			return play(t, seats(t, transfer, 0, nil, "", "7H 9D", "7S 8H", "QC KC AC"), "attack 7H")
		}, 1, "defend 7H 8H", false},
		{"passes when nothing fits", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "6D 7D 8D 9D TD JD", "7H 8C QH", "9H TH AD"), "attack 7H", "defend 7H 9H")
		}, 0, "pass", false},
		{"throws in cheap cards", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "6D 7D 8D 9D TD JD", "7H 7C QH", "9H TH AD"), "attack 7H", "defend 7H 9H")
		}, 0, "attack 7C", false},
		{"keeps trumps while the stock lasts", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "6D 7D 8D 9D TD JD", "7H 7S AH", "9H AD TH"), "attack 7H", "defend 7H 9H")
		}, 0, "pass", false},
		{"dumps anything that fits once the stock is gone", func(t *testing.T) Position {
			return play(t, seats(t, DefaultOptions(0), 0, nil, "", "7H 7S QH", "9H TH AD"), "attack 7H", "defend 7H 9H")
		}, 0, "attack 7S", false},
		{"catches a throw-in of a rank that was not on the table", func(t *testing.T) Position {
			return play(t, cheatTable(t, cheating), "attack 7H", "defend 7H 8H", "attack 9C")
		}, 2, "catch 9C", false},
		{"catches a defence that does not beat", func(t *testing.T) Position {
			return play(t, cheatTable(t, cheating), "attack 7H", "defend 7H 6C")
		}, 2, "catch 6C", false},
		{"the attacker on turn catches before it plays", func(t *testing.T) Position {
			return play(t, cheatTable(t, cheating), "attack 7H", "defend 7H 6C")
		}, 0, "catch 6C", false},
		{"leaves honest cards alone", func(t *testing.T) Position {
			return play(t, cheatTable(t, cheating), "attack 7H", "defend 7H 8H")
		}, 2, "", true},
		{"has nothing to do out of turn", func(t *testing.T) Position {
			return play(t, cheatTable(t, DefaultOptions(0)), "attack 7H")
		}, 2, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.p(t)
			m, ok := p.Bot(c.seat, 0)
			if c.quiet {
				if ok {
					t.Fatalf("the bot played %s", m)
				}
				return
			}
			if !ok || m.String() != c.want {
				t.Fatalf("bot played %q (%v), want %q", m, ok, c.want)
			}
			if !slices.Contains(p.LegalFor(c.seat), m) {
				t.Fatalf("%s is not legal: %v", m, legal(p))
			}
		})
	}
	lead := seats(t, DefaultOptions(0), 0, nil, "", "7C 7D 7H KS", "8H 9H")
	seen := map[string]bool{}
	for seed := range uint64(12) {
		m, _ := lead.Bot(0, seed)
		seen[m.String()] = true
		if again, _ := lead.Bot(0, seed); again != m {
			t.Fatal("the bot is not a pure function of the seed")
		}
	}
	if len(seen) != 3 {
		t.Fatal("the seed does not break ties between equal leads", seen)
	}
}

func TestBotMoveThroughTheEngine(t *testing.T) {
	roster := []engine.PlayerID{"a", "b", "c", "d"}
	state, err := New().InitConfigured(roster, 21, []byte(`{"mode":"transfer"}`))
	if err != nil {
		t.Fatal(err)
	}
	m, err := New().BotMove(state, roster[state.Turn], 4)
	if err != nil || !slices.Contains(notation(New().ValidMoves(state, roster[state.Turn])), notation([]engine.Move{m})[0]) || m.Ply != state.Ply || m.Player != roster[state.Turn] {
		t.Fatalf("bot move %+v, %v", m, err)
	}
	idle := roster[(state.Turn+2)%4]
	if _, err := New().BotMove(state, idle, 4); !errors.Is(err, engine.ErrOutOfTurn) {
		t.Fatal("a bot out of turn moved", err)
	}
	if _, err := New().BotMove(state, "spectator", 4); !errors.Is(err, engine.ErrPlayerNotInGame) {
		t.Fatal("a stranger got a bot move", err)
	}
	if _, err := New().BotMove(state.Finish(0), roster[0], 4); !errors.Is(err, engine.ErrGameOver) {
		t.Fatal("a bot moved after the game", err)
	}
}

type tableRun struct {
	plies  int
	caught int
}

func checkPosition(t *testing.T, p Position, label string) {
	t.Helper()
	all := p.Cards()
	slices.SortFunc(all, byIdentity)
	if !slices.Equal(all, DeckOf(p.Options.Deck)) {
		t.Fatalf("%s: the deck was not conserved", label)
	}
	for _, pair := range p.Table {
		if p.dishonest(pair.Attack) != (pair.Cheat&MarkAttack != 0) || pair.Covered && p.dishonest(pair.Defense) != (pair.Cheat&MarkDefense != 0) {
			t.Fatalf("%s: public evidence disagrees with the cheat marks %+v", label, p.Table)
		}
	}
}

func checkPlaces(t *testing.T, p Position, label string) {
	t.Helper()
	over, winner := p.Outcome()
	n := len(p.Hands)
	if !over || len(p.Places) != n || len(p.Table) != 0 || len(p.Stock) != 0 {
		t.Fatalf("%s: unfinished %+v", label, p)
	}
	ranking := slices.Sorted(slices.Values(p.Places))
	for i, place := range ranking {
		if place != i+1 && (i == 0 || place != ranking[i-1]) {
			t.Fatalf("%s: places %v are not a ranking", label, p.Places)
		}
	}
	durak := p.Durak()
	for seat, place := range p.Places {
		holds := len(p.Hands[seat]) > 0
		if (place == n) != (seat == durak) || seat != durak && holds || seat == durak && !holds && p.Options.Draw {
			t.Fatalf("%s: places %v, durak %d, hands %v", label, p.Places, durak, p.Hands)
		}
	}
	if durak != engine.NoSeat && (winner < 0 || p.Places[winner] != 1) || durak == engine.NoSeat && (winner != engine.NoSeat || !p.Options.Draw) {
		t.Fatalf("%s: winner %d places %v", label, winner, p.Places)
	}
}

func cheats(p Position, seat int) []Move {
	var out []Move
	for _, c := range p.Hands[seat] {
		if m := (Move{Action: Attack, Card: c}); p.CheatAllowed(seat, m) {
			out = append(out, m)
		}
		for _, pair := range p.Table {
			if m := (Move{Action: Defend, Card: c, Against: pair.Attack}); !pair.Covered && p.CheatAllowed(seat, m) {
				out = append(out, m)
			}
		}
	}
	return out
}

func runTable(t *testing.T, o Options, seed uint64, cheater int) tableRun {
	t.Helper()
	label := fmt.Sprintf("%+v seed %d", o, seed)
	rng := rand.New(rand.NewPCG(seed, 77))
	p := DealTable(seed, o)
	var run tableRun
	for over, _ := p.Outcome(); !over; over, _ = p.Outcome() {
		if run.plies++; run.plies > 20*MaxPlies {
			t.Fatalf("%s: no end in sight", label)
		}
		checkPosition(t, p, label)
		seat := p.Turn()
		m, ok := p.Bot(seat, seed+uint64(run.plies))
		switch {
		case !ok:
			t.Fatalf("%s: the bot on turn has no move", label)
		case seat == cheater && rng.IntN(3) == 0 && len(cheats(p, seat)) > 0:
			options := cheats(p, seat)
			m = options[rng.IntN(len(options))]
		case !slices.Contains(p.LegalFor(seat), m):
			t.Fatalf("%s ply %d: the bot played %s, legal %v", label, run.plies, m, p.LegalFor(seat))
		}
		next, err := p.Play(seat, m)
		if err != nil {
			t.Fatalf("%s ply %d: %s: %v", label, run.plies, m, err)
		}
		p = next
		for watcher := range p.Hands {
			if watcher == cheater {
				continue
			}
			catch, ok := p.Bot(watcher, seed)
			if !ok || catch.Action != Catch {
				continue
			}
			if cheater < 0 {
				t.Fatalf("%s: an honest table produced a catch %s", label, catch)
			}
			if p, err = p.Play(watcher, catch); err != nil {
				t.Fatalf("%s: catch %s: %v", label, catch, err)
			}
			run.caught++
			break
		}
	}
	checkPlaces(t, p, label)
	return run
}

func tableOptions() []Options {
	var out []Options
	for n := MinSeats; n <= MaxSeats; n++ {
		for _, deck := range []int{24, 36, 52} {
			if n*HandSize > deck {
				continue
			}
			for _, mode := range []Mode{ModeThrowIn, ModeTransfer} {
				for _, throwIn := range []ThrowIn{ThrowInAll, ThrowInNeighbors} {
					for _, draw := range []bool{true, false} {
						for _, first := range []int{5, 6} {
							for _, cheats := range []bool{false, true} {
								out = append(out, Options{Mode: mode, Cheats: cheats, Deck: deck, ThrowIn: throwIn, Draw: draw, FirstBoutLimit: first, Seats: n})
							}
						}
					}
				}
			}
		}
	}
	return out
}

func TestBotsFinishEveryTable(t *testing.T) {
	longest := map[int]int{}
	for i, o := range tableOptions() {
		for k := range uint64(5) {
			seed := uint64(i) + k<<32
			run := runTable(t, o, seed, -1)
			longest[o.Seats] = max(longest[o.Seats], run.plies)
		}
	}
	for n, plies := range longest {
		if plies >= MaxPlies {
			t.Fatalf("%d seats took %d plies, the cap is %d", n, plies, MaxPlies)
		}
	}
	t.Logf("longest bot game by seats: %v", longest)
}

func TestBotsCatchACheater(t *testing.T) {
	caught := 0
	for i, o := range tableOptions() {
		if !o.Cheats {
			continue
		}
		run := runTable(t, o, uint64(i)*13, i%o.Seats)
		caught += run.caught
	}
	if caught == 0 {
		t.Fatal("bots never caught the cheater")
	}
	t.Logf("cheats caught: %d", caught)
}

func TestRandomTablesTerminate(t *testing.T) {
	for i, o := range tableOptions() {
		rng := rand.New(rand.NewPCG(uint64(i), 3))
		p := DealTable(uint64(i)*101, o)
		label := fmt.Sprintf("%+v", o)
		for plies := 0; ; plies++ {
			if over, _ := p.Outcome(); over {
				break
			}
			if plies > 50*MaxPlies {
				t.Fatalf("%s: random play does not end", label)
			}
			checkPosition(t, p, label)
			seat := p.Turn()
			if o.Cheats && rng.IntN(4) == 0 {
				seat = rng.IntN(len(p.Hands))
			}
			moves := p.LegalFor(seat)
			if len(moves) == 0 {
				seat = p.Turn()
				moves = p.Legal()
			}
			if len(moves) == 0 {
				t.Fatalf("%s: seat %d to act has no moves", label, seat)
			}
			next, err := p.Play(seat, moves[rng.IntN(len(moves))])
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			p = next
		}
		checkPlaces(t, p, label)
	}
}

func TestBotGamesThroughTheEngine(t *testing.T) {
	roster := []engine.PlayerID{"a", "b", "c", "d", "e", "f"}
	for n := MinSeats; n <= MaxSeats; n++ {
		for _, raw := range []string{`{"mode":"transfer","cheats":true}`, `{"throw_in":"neighbors","draw":false,"first_bout_limit":5}`} {
			players := roster[:n]
			state, err := New().InitConfigured(players, uint64(n)*7, []byte(raw))
			if err != nil {
				t.Fatal(err)
			}
			state.MaxPlies = MaxPlies
			for !state.Over() {
				player := players[state.Turn]
				m, err := New().BotMove(state, player, uint64(state.Ply))
				if err != nil {
					t.Fatalf("%d seats %s ply %d: %v", n, raw, state.Ply, err)
				}
				if !slices.Contains(notation(New().ValidMoves(state, player)), notation([]engine.Move{m})[0]) {
					t.Fatalf("%d seats: bot move %s is not valid", n, notation([]engine.Move{m}))
				}
				if state, err = New().ApplyMove(state, m); err != nil {
					t.Fatal(err)
				}
			}
			d, p := full(t, state)
			if state.MoveLimit || len(d.Places) != n || d.End == Ongoing {
				t.Fatalf("%d seats %s: ended %q after %d plies, places %v", n, raw, d.End, state.Ply, d.Places)
			}
			if over, winner := p.Outcome(); !over || winner != state.Winner || (d.End == Drawn) != (state.Status == engine.StatusDraw) {
				t.Fatalf("%d seats: outcome %d vs state %d/%s", n, winner, state.Winner, state.Status)
			}
			snap, err := Describe(New().View(state, "spectator"))
			if err != nil || !slices.Equal(snap.Places, d.Places) || len(snap.Hands) != n {
				t.Fatalf("%d seats: final snapshot %+v, %v", n, snap, err)
			}
		}
	}
}

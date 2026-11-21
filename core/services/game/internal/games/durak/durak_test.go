package durak

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

var players = []engine.PlayerID{"north", "south"}

func full(t *testing.T, state engine.State) (data, Position) {
	t.Helper()
	d, p, err := decode(state)
	if err != nil {
		t.Fatal(err)
	}
	return d, p
}

func notation(moves []engine.Move) []string {
	out := make([]string, 0, len(moves))
	for _, m := range moves {
		var in MoveData
		_ = json.Unmarshal(m.Data, &in)
		out = append(out, in.Move)
	}
	return out
}

func apply(t *testing.T, state engine.State, raw string) engine.State {
	t.Helper()
	next, err := New().ApplyMove(state, NewMove(state.Players[state.Turn], state.Ply, raw))
	if err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return next
}

func TestInitSeeded(t *testing.T) {
	state := New().InitSeeded(players, 7)
	if state.Game != Key || state.Seed != 7 || state.Over() || state.Ply != 0 {
		t.Fatalf("initial state %+v", state)
	}
	d, p := full(t, state)
	if state.Turn != p.Attacker || d.Left != 24 || d.Held != [2]int{6, 6} || d.Trump != "JD" || len(d.Moves) != 0 {
		t.Fatalf("initial data %+v", d)
	}
	if moves := New().ValidMoves(state, players[1-state.Turn]); moves != nil {
		t.Fatal("the defender has moves before an attack", notation(moves))
	}
	if got := notation(New().ValidMoves(state, players[state.Turn])); len(got) != 6 || !strings.HasPrefix(got[0], "attack ") {
		t.Fatal("opening moves", got)
	}
	games := engine.NewRegistry()
	if err := games.Register(Definition()); err != nil {
		t.Fatal(err)
	}
	if _, err := games.NewState(Key, players); !errors.Is(err, engine.ErrSeedRequired) {
		t.Fatal("dealt without a seed", err)
	}
	seeded, err := games.NewSeededState(Key, players, 7)
	if err != nil || seeded.Seed != 7 || string(seeded.Data) != string(state.Data) {
		t.Fatal("registry deal", seeded, err)
	}
}

func TestApplyMoveRejects(t *testing.T) {
	state := New().InitSeeded(players, 7)
	attacker, defender := players[state.Turn], players[1-state.Turn]
	cases := []struct {
		name string
		move engine.Move
		want error
	}{
		{"defender out of turn", NewMove(defender, 0, "take"), engine.ErrOutOfTurn},
		{"stale ply", NewMove(attacker, 3, "attack 6S"), engine.ErrStaleMove},
		{"card not in hand", NewMove(attacker, 0, "attack 7S"), ErrIllegalMove},
		{"take on an empty table", NewMove(attacker, 0, "take"), ErrIllegalMove},
		{"pass on an empty table", NewMove(attacker, 0, "pass"), ErrIllegalMove},
		{"garbage", NewMove(attacker, 0, "attack 7♥"), ErrBadMove},
		{"bad json", engine.Move{Player: attacker, Ply: 0, Data: json.RawMessage(`{"move":`)}, engine.ErrMoveMalformed},
		{"stranger", NewMove("someone", 0, "pass"), engine.ErrPlayerNotInGame},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := New().ApplyMove(state, c.move); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
	view := New().View(state, attacker)
	if _, err := New().ApplyMove(view, NewMove(attacker, 0, "attack 6S")); !errors.Is(err, engine.ErrStateMalformed) {
		t.Fatal("played on a redacted view", err)
	}
	broken := state
	broken.Data = json.RawMessage(strings.Replace(string(state.Data), `"JD"`, `"6S"`, 1))
	if _, err := New().ApplyMove(broken, NewMove(attacker, 0, "attack 6C")); !errors.Is(err, engine.ErrStateMalformed) {
		t.Fatal("accepted a deck with a duplicated card", err)
	}
}

func TestBoutThroughTheEngine(t *testing.T) {
	state := New().InitSeeded(players, 7)
	if state.Turn != 1 {
		t.Fatalf("seed 7: seat 1 holds the lowest trump 6D, attacker %d", state.Turn)
	}
	state = apply(t, state, "attack 6H")
	if state.Turn != 0 {
		t.Fatal("the defender answers")
	}
	if got := notation(New().ValidMoves(state, players[0])); !slices.Equal(got, []string{"defend 6H AH", "defend 6H KD", "take"}) {
		t.Fatal("defences", got)
	}
	state = apply(t, state, "defend 6H AH")
	state = apply(t, state, "attack 6D")
	state = apply(t, state, "take")
	if got := notation(New().ValidMoves(state, players[1])); !slices.Equal(got, []string{"attack AC", "pass"}) {
		t.Fatal("throw-ins after a take", got)
	}
	state = apply(t, state, "pass")
	d, p := full(t, state)
	if state.Turn != 1 || p.Attacker != 1 || d.Held != [2]int{8, 6} || d.Left != 22 {
		t.Fatalf("after the take: turn %d held %v left %d", state.Turn, d.Held, d.Left)
	}
	if !slices.Equal(d.Moves, []string{"attack 6H", "defend 6H AH", "attack 6D", "take", "pass"}) {
		t.Fatal("notation", d.Moves)
	}
}

func TestPlyCap(t *testing.T) {
	if def := Definition(); def.MaxPlies != MaxPlies {
		t.Fatalf("definition caps at %d, want %d", def.MaxPlies, MaxPlies)
	}
	state := New().InitSeeded(players, 7)
	state.MaxPlies = MaxPlies
	state.Ply = MaxPlies - 1
	next, err := New().ApplyMove(state, NewMove(players[state.Turn], state.Ply, "attack 6H"))
	if err != nil {
		t.Fatal(err)
	}
	if next.Status != engine.StatusDraw || next.Winner != engine.NoSeat || next.Turn != engine.NoSeat || !next.MoveLimit {
		t.Fatalf("capped game %+v", next)
	}
	snap, err := Describe(next)
	if err != nil || snap.End != Ongoing || snap.Legal != nil {
		t.Fatal(snap.End, snap.Legal, err)
	}
	if _, err := New().ApplyMove(next, NewMove(players[0], next.Ply, "take")); !errors.Is(err, engine.ErrGameOver) {
		t.Fatal("played after the cap", err)
	}
}

func TestViewHidesTheOpponentAndTheStock(t *testing.T) {
	state := apply(t, New().InitSeeded(players, 7), "attack 6H")
	d, _ := full(t, state)
	cases := []struct {
		name    string
		viewer  engine.PlayerID
		visible [2]bool
		legal   bool
	}{
		{"the defender", players[0], [2]bool{true, false}, true},
		{"the attacker", players[1], [2]bool{false, true}, false},
		{"a spectator", "someone", [2]bool{false, false}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view := New().View(state, c.viewer)
			if view.Seed != 0 {
				t.Fatal("the seed left the engine")
			}
			raw := string(view.Data)
			for _, hidden := range d.Stock[:len(d.Stock)-1] {
				if strings.Contains(raw, `"`+hidden+`"`) {
					t.Fatalf("stock card %s leaked: %s", hidden, raw)
				}
			}
			snap, err := Describe(view)
			if err != nil {
				t.Fatal(err)
			}
			for seat := range 2 {
				hand := snap.Hands[seat]
				if hand.Count != d.Held[seat] || hand.Hidden == c.visible[seat] {
					t.Fatalf("seat %d: %+v", seat, hand)
				}
				if c.visible[seat] != (len(hand.Cards) == d.Held[seat]) {
					t.Fatalf("seat %d cards %v", seat, hand.Cards)
				}
				if !c.visible[seat] {
					for _, secret := range d.Hands[seat] {
						if strings.Contains(raw, `"`+secret+`"`) {
							t.Fatalf("hidden card %s leaked: %s", secret, raw)
						}
					}
				}
			}
			if snap.Stock != 24 || snap.Trump != "JD" || len(snap.Table) != 1 || snap.Table[0].Attack != "6H" {
				t.Fatalf("public part %+v", snap)
			}
			if (len(snap.Legal) > 0) != c.legal {
				t.Fatalf("legal moves %v", snap.Legal)
			}
		})
	}
	unredacted, err := Describe(state)
	if err != nil || len(unredacted.Legal) != 0 || unredacted.Hands[0].Hidden || unredacted.Hands[1].Hidden {
		t.Fatal("an unredacted live state must not advertise moves", unredacted, err)
	}
	over := state.Finish(0)
	if revealed := New().View(over, "someone"); string(revealed.Data) != string(over.Data) || revealed.Seed != 0 {
		t.Fatal("a finished game is revealed in full apart from the seed")
	}
	bad := state
	bad.Data = json.RawMessage(`{"hands":`)
	if view := New().View(bad, players[0]); view.Data != nil {
		t.Fatal("an unreadable state must fail closed", string(view.Data))
	}
}

func TestRandomPlayTerminatesAndConservesCards(t *testing.T) {
	ends := map[End]int{}
	longest := 0
	for seed := range uint64(300) {
		rng := rand.New(rand.NewPCG(seed, 99))
		state := New().InitSeeded(players, seed*7919)
		for !state.Over() {
			if state.Ply >= MaxPlies {
				t.Fatalf("seed %d ran past the cap", seed)
			}
			_, p := full(t, state)
			if got := p.Cards(); len(got) != DeckSize {
				t.Fatalf("seed %d ply %d: %d cards", seed, state.Ply, len(got))
			}
			moves := New().ValidMoves(state, players[state.Turn])
			if len(moves) == 0 {
				t.Fatalf("seed %d ply %d: the player to act has no moves", seed, state.Ply)
			}
			if other := New().ValidMoves(state, players[1-state.Turn]); other != nil {
				t.Fatalf("seed %d ply %d: both players may act", seed, state.Ply)
			}
			next, err := New().ApplyMove(state, moves[rng.IntN(len(moves))])
			if err != nil {
				t.Fatalf("seed %d ply %d: %v", seed, state.Ply, err)
			}
			state = next
		}
		d, p := full(t, state)
		all := p.Cards()
		slices.SortFunc(all, func(a, b Card) int { return int(a.Suit)*16 + int(a.Rank) - int(b.Suit)*16 - int(b.Rank) })
		if !slices.Equal(all, Deck()) {
			t.Fatalf("seed %d: the deck was not conserved", seed)
		}
		ends[d.End]++
		longest = max(longest, state.Ply)
		switch d.End {
		case Lost:
			if state.Status != engine.StatusWin || len(p.Hands[state.Winner]) != 0 || len(p.Hands[1-state.Winner]) == 0 || len(p.Stock) != 0 {
				t.Fatalf("seed %d: loss %+v", seed, d)
			}
		case Drawn:
			if state.Status != engine.StatusDraw || len(p.Hands[0])+len(p.Hands[1])+len(p.Stock) != 0 {
				t.Fatalf("seed %d: draw %+v", seed, d)
			}
		default:
			t.Fatalf("seed %d ended with %q after %d plies", seed, d.End, state.Ply)
		}
	}
	if ends[Lost] == 0 || longest >= MaxPlies {
		t.Fatalf("ends %v, longest %d", ends, longest)
	}
	t.Logf("ends %v, longest game %d plies", ends, longest)
}

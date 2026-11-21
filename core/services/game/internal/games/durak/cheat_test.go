package durak

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

var cheating = with(func(o *Options) { o.Cheats = true })

func cheatTable(t *testing.T, o Options) Position {
	t.Helper()
	return seats(t, o, 0, nil, "QH", "7H 9C 6S", "8H 6C 8D", "TC JC QD")
}

func TestCheatMoves(t *testing.T) {
	cases := []struct {
		name  string
		moves []string
		cheat string
		mark  Mark
	}{
		{"a defence that does not beat", []string{"attack 7H"}, "defend 7H 6C", MarkDefense},
		{"a throw-in of a rank that is not on the table", []string{"attack 7H", "defend 7H 8H"}, "attack 9C", MarkAttack},
		{"a cheat throw-in while the defender takes", []string{"attack 7H", "take"}, "attack 9C", MarkAttack},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			honest := play(t, cheatTable(t, DefaultOptions(0)), c.moves...)
			if _, err := honest.Play(honest.Turn(), move(t, c.cheat)); !errors.Is(err, ErrIllegalMove) {
				t.Fatal("a table without cheats accepted", c.cheat, err)
			}
			if honest.CheatAllowed(honest.Turn(), move(t, c.cheat)) {
				t.Fatal("cheating allowed at an honest table")
			}
			p := play(t, cheatTable(t, cheating), c.moves...)
			if slices.Contains(legal(p), c.cheat) || !p.CheatAllowed(p.Turn(), move(t, c.cheat)) {
				t.Fatal("cheats are not legal moves but are allowed", legal(p))
			}
			p = play(t, p, c.cheat)
			if got := p.Table[len(p.Table)-1].Cheat; got != c.mark && p.Table[0].Cheat != c.mark {
				t.Fatalf("cheat mark %v", p.Table)
			}
		})
	}
	p := cheatTable(t, cheating)
	if p.CheatAllowed(0, move(t, "attack 9C")) {
		t.Fatal("a lead is never a cheat")
	}
	p = play(t, p, "attack 7H", "defend 7H 8H")
	if p.CheatAllowed(0, move(t, "defend 7H 9C")) || p.CheatAllowed(2, move(t, "attack TC")) || p.CheatAllowed(0, move(t, "attack 8D")) {
		t.Fatal("cheats out of turn or with cards not in hand")
	}
	full := play(t, seats(t, cheating, 0, nil, "", "7H 9C", "8H", "TC"), "attack 7H", "defend 7H 8H")
	if _, err := full.Play(0, move(t, "attack 9C")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("a cheat went past the bout limit", err)
	}
}

func TestCatch(t *testing.T) {
	cases := []struct {
		name     string
		o        Options
		moves    []string
		catcher  int
		catch    string
		illegal  bool
		caught   int
		attacker int
		turn     int
	}{
		{"a caught thrower takes the table and the defender attacks", cheating, []string{"attack 7H", "defend 7H 8H", "attack 9C"}, 2, "9C", false, 0, 1, 1},
		{"a caught defender takes the table and is skipped", cheating, []string{"attack 7H", "defend 7H 6C"}, 0, "6C", false, 1, 2, 2},
		{"the defender catches a thrower", cheating, []string{"attack 7H", "defend 7H 8H", "attack 9C"}, 1, "9C", false, 0, 1, 1},
		{"a false accusation only marks the card", cheating, []string{"attack 7H", "defend 7H 8H"}, 2, "8H", false, engine.NoSeat, 0, 0},
		{"a cheat on a taken bout is caught too", cheating, []string{"attack 7H", "take", "attack 9C"}, 2, "9C", false, 0, 1, 1},
		{"nobody accuses their own card", cheating, []string{"attack 7H", "defend 7H 6C"}, 1, "6C", true, 0, 0, 0},
		{"a thrower does not accuse its own throw", cheating, []string{"attack 7H", "defend 7H 8H", "attack 9C"}, 0, "9C", true, 0, 0, 0},
		{"no catching at an honest table", DefaultOptions(0), []string{"attack 7H", "defend 7H 8H"}, 2, "8H", true, 0, 0, 0},
		{"only cards on the table", cheating, []string{"attack 7H", "defend 7H 8H"}, 2, "9C", true, 0, 0, 0},
		{"no catching after the bout", cheating, []string{"attack 7H", "defend 7H 6C", "pass", "pass"}, 0, "6C", true, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := play(t, cheatTable(t, c.o), c.moves...)
			before := p
			next, err := p.Play(c.catcher, move(t, "catch "+c.catch))
			if c.illegal {
				if !errors.Is(err, ErrIllegalMove) || slices.Contains(names(cardsOf(p.LegalFor(c.catcher))), c.catch) {
					t.Fatalf("catch accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.caught == engine.NoSeat {
				if !slices.Equal(next.Hands[0], before.Hands[0]) || len(next.Table) != len(before.Table) || next.Turn() != before.Turn() || next.Table[0].Checked != MarkDefense {
					t.Fatalf("a false accusation changed the bout: %+v", next)
				}
				if _, err := next.Play(0, move(t, "catch "+c.catch)); !errors.Is(err, ErrIllegalMove) {
					t.Fatal("a cleared card was accused twice", err)
				}
				return
			}
			for _, pair := range before.Table {
				if !slices.Contains(next.Hands[c.caught], pair.Attack) || pair.Covered && !slices.Contains(next.Hands[c.caught], pair.Defense) {
					t.Fatalf("seat %d did not pick up the table: %s", c.caught, hand(next, c.caught))
				}
			}
			if len(next.Table) != 0 || len(next.Discard) != 0 || next.Attacker != c.attacker || next.Turn() != c.turn {
				t.Fatalf("after the catch: attacker %d turn %d table %v", next.Attacker, next.Turn(), next.Table)
			}
		})
	}
	out := play(t, seats(t, cheating, 1, []int{1, 0, 0}, "", "", "7H 9C", "8H 6D"), "attack 7H", "defend 7H 6D")
	if _, err := out.Play(0, move(t, "catch 6D")); !errors.Is(err, ErrIllegalMove) {
		t.Fatal("a seat that went out caught a cheat", err)
	}
}

func cardsOf(moves []Move) []Card {
	out := make([]Card, 0, len(moves))
	for _, m := range moves {
		out = append(out, m.Card)
	}
	return out
}

func cheatDeal(t *testing.T, roster []engine.PlayerID) (engine.State, string, string) {
	t.Helper()
	for seed := range uint64(200) {
		state, err := New().InitConfigured(roster, seed, []byte(`{"cheats":true}`))
		if err != nil {
			t.Fatal(err)
		}
		_, p := full(t, state)
		lead := p.Hands[p.Attacker][0]
		after, err := p.Play(p.Attacker, Move{Action: Attack, Card: lead})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range after.Hands[after.Defender()] {
			if after.CheatAllowed(after.Defender(), Move{Action: Defend, Card: c, Against: lead}) {
				return state, lead.String(), c.String()
			}
		}
	}
	t.Fatal("no deal lets the defender cheat")
	return engine.State{}, "", ""
}

func TestCheatingThroughTheEngine(t *testing.T) {
	roster := []engine.PlayerID{"a", "b", "c"}
	state, lead, cheat := cheatDeal(t, roster)
	state = apply(t, state, "attack "+lead)
	defender := roster[state.Turn]
	if slices.Contains(notation(New().ValidMoves(state, defender)), "defend "+lead+" "+cheat) {
		t.Fatal("a cheat is offered as a legal move")
	}
	state = apply(t, state, "defend "+lead+" "+cheat)
	if !strings.Contains(string(state.Data), `"cheat":["`+cheat+`"]`) {
		t.Fatal("the cheat is not marked in the state", string(state.Data))
	}
	_, p := full(t, state)
	witness := roster[3-p.Attacker-p.Defender()]
	if state.Turn == slices.Index(roster, witness) {
		t.Fatal("the witness should be out of turn")
	}
	for _, viewer := range append(slices.Clone(roster), "spectator") {
		view := New().View(state, viewer)
		if strings.Contains(string(view.Data), `"cheat"`) {
			t.Fatalf("%s sees the cheat mark: %s", viewer, view.Data)
		}
		snap, err := Describe(view)
		if err != nil {
			t.Fatal(err)
		}
		if viewer == witness && !slices.Contains(snap.Legal, "catch "+cheat) {
			t.Fatalf("the witness cannot catch out of turn: %v", snap.Legal)
		}
	}
	if got := notation(New().ValidMoves(state, witness)); !slices.Equal(got, []string{"catch " + lead, "catch " + cheat}) {
		t.Fatal("moves of a seat out of turn at a cheating table", got)
	}
	cleared, err := New().ApplyMove(state, NewMove(witness, state.Ply, "catch "+lead))
	if err != nil || cleared.Turn != state.Turn || cleared.Ply != state.Ply+1 {
		t.Fatalf("a false accusation: turn %d ply %d, %v", cleared.Turn, cleared.Ply, err)
	}
	if snap, err := Describe(New().View(cleared, witness)); err != nil || !slices.Equal(snap.Table[0].Checked, []string{lead}) || slices.Contains(snap.Legal, "catch "+lead) {
		t.Fatalf("a cleared card stays cleared: %+v %v", snap.Table, err)
	}
	caught, err := New().ApplyMove(state, NewMove(witness, state.Ply, "catch "+cheat))
	if err != nil {
		t.Fatal(err)
	}
	d, after := full(t, caught)
	if len(after.Table) != 0 || after.Attacker != (p.Defender()+1)%3 || caught.Turn != after.Attacker || d.Moves[len(d.Moves)-1] != "catch "+cheat {
		t.Fatalf("after the catch: attacker %d turn %d moves %v", after.Attacker, caught.Turn, d.Moves)
	}
	rejects := []struct {
		name string
		move engine.Move
		want error
	}{
		{"a stranger", NewMove("spectator", state.Ply, "catch "+cheat), engine.ErrPlayerNotInGame},
		{"a stale catch", NewMove(witness, state.Ply-1, "catch "+cheat), engine.ErrStaleMove},
		{"a move out of turn", NewMove(witness, state.Ply, "pass"), engine.ErrOutOfTurn},
		{"a catch of a card in hand", NewMove(witness, state.Ply, "catch "+p.Hands[slices.Index(roster, witness)][0].String()), ErrIllegalMove},
	}
	for _, c := range rejects {
		if _, err := New().ApplyMove(state, c.move); !errors.Is(err, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	over := caught.Finish(0)
	if _, err := New().ApplyMove(over, NewMove(witness, over.Ply, "catch "+cheat)); !errors.Is(err, engine.ErrGameOver) {
		t.Fatal("caught after the game", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(New().View(state, witness).Data, &raw); err != nil || raw["viewer"] == nil {
		t.Fatal("the view is not marked", err)
	}
}

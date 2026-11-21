package durak

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func TestParseOptions(t *testing.T) {
	everything := Options{Mode: ModeTransfer, Cheats: true, Deck: 52, ThrowIn: ThrowInNeighbors, Draw: false, FirstBoutLimit: 5, Seats: 6}
	cases := []struct {
		name  string
		raw   string
		seats int
		want  Options
		bad   bool
	}{
		{"nothing is the classic table", "", 2, DefaultOptions(2), false},
		{"null is the classic table", "null", 3, DefaultOptions(3), false},
		{"an empty object keeps every default", " {} ", 0, DefaultOptions(0), false},
		{"every option", `{"mode":"transfer","cheats":true,"deck":52,"throw_in":"neighbors","draw":false,"first_bout_limit":5,"seats":6}`, 6, everything, false},
		{"seats come from the roster", `{"mode":"transfer"}`, 4, with(func(o *Options) { o.Mode, o.Seats = ModeTransfer, 4 }), false},
		{"unknown option", `{"jokers":true}`, 2, Options{}, true},
		{"trailing data", `{} {}`, 2, Options{}, true},
		{"not an object", `[1]`, 2, Options{}, true},
		{"unknown mode", `{"mode":"podkidnoy"}`, 2, Options{}, true},
		{"unknown deck", `{"deck":40}`, 2, Options{}, true},
		{"unknown throw-in", `{"throw_in":"left"}`, 2, Options{}, true},
		{"first bout of four", `{"first_bout_limit":4}`, 2, Options{}, true},
		{"one seat", `{"seats":1}`, 0, Options{}, true},
		{"seven seats", `{"seats":7}`, 0, Options{}, true},
		{"five seats cannot share 24 cards", `{"deck":24,"seats":5}`, 0, Options{}, true},
		{"four seats share 24 cards", `{"deck":24,"seats":4}`, 0, with(func(o *Options) { o.Deck, o.Seats = 24, 4 }), false},
		{"seats differ from the roster", `{"seats":3}`, 4, Options{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseOptions([]byte(c.raw), c.seats)
			if c.bad {
				if !errors.Is(err, engine.ErrInvalidOptions) {
					t.Fatalf("err = %v, want ErrInvalidOptions", err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("options %+v, %v, want %+v", got, err, c.want)
			}
			if c.seats == 0 {
				if err := New().ValidateOptions([]byte(c.raw)); err != nil {
					t.Fatal("ValidateOptions disagrees", err)
				}
			}
		})
	}
	if err := New().ValidateOptions([]byte(`{"deck":24,"seats":6}`)); !errors.Is(err, engine.ErrInvalidOptions) {
		t.Fatal("ValidateOptions let six seats share 24 cards", err)
	}
}

func TestInitConfigured(t *testing.T) {
	four := []engine.PlayerID{"a", "b", "c", "d"}
	state, err := New().InitConfigured(four, 5, []byte(`{"mode":"transfer","deck":24,"cheats":true}`))
	if err != nil {
		t.Fatal(err)
	}
	d, p := full(t, state)
	want := with(func(o *Options) { o.Mode, o.Deck, o.Cheats, o.Seats = ModeTransfer, 24, true, 4 })
	if state.Seed != 5 || state.Turn != p.Turn() || state.Turn != p.Attacker || len(state.Players) != 4 {
		t.Fatalf("configured state %+v", state)
	}
	if d.Options == nil || *d.Options != want || !slices.Equal(d.Held, []int{6, 6, 6, 6}) || d.Left != 0 || len(d.Hands) != 4 {
		t.Fatalf("configured data %+v", d)
	}
	if moves := New().ValidMoves(state, four[state.Turn]); len(moves) != 6 {
		t.Fatal("the first attacker may lead any card", notation(moves))
	}
	if _, err := New().InitConfigured(four[:1], 5, nil); !errors.Is(err, engine.ErrPlayerCount) {
		t.Fatal("dealt to one player", err)
	}
	if _, err := New().InitConfigured(append(four, "e", "f", "g"), 5, nil); !errors.Is(err, engine.ErrPlayerCount) {
		t.Fatal("dealt to seven players", err)
	}
	if _, err := New().InitConfigured(append(four, "e"), 5, []byte(`{"deck":24}`)); !errors.Is(err, engine.ErrInvalidOptions) {
		t.Fatal("dealt 30 cards out of 24", err)
	}
	trio := New().InitSeeded(four[:3], 5)
	if _, p := full(t, trio); p.Options != DefaultOptions(3) || len(p.Hands) != 3 || len(p.Stock) != 18 {
		t.Fatal("a seeded deal without options is a classic table", p.Options)
	}
	if def := Definition(); def.MinPlayers != MinSeats || def.MaxPlayers != MaxSeats {
		t.Fatalf("definition seats %d..%d", def.MinPlayers, def.MaxPlayers)
	}
	games := engine.NewRegistry()
	if err := games.Register(Definition()); err != nil {
		t.Fatal(err)
	}
	for n := MinSeats; n <= MaxSeats; n++ {
		roster := []engine.PlayerID{"a", "b", "c", "d", "e", "f"}[:n]
		if state, err := games.NewSeededState(Key, roster, 3); err != nil || len(state.Players) != n || state.Over() {
			t.Fatalf("%d seats: %v", n, err)
		}
	}
	if _, err := games.NewSeededState(Key, append(four, "e", "f", "g"), 3); !errors.Is(err, engine.ErrPlayerCount) {
		t.Fatal("the registry seated seven", err)
	}
}

const legacyState = `{"game":"durak","players":["north","south"],"turn":1,"ply":4,"status":"in_progress","winner":-1,"seed":"7","data":{"hands":[["6S","6C","8C","9C","KD"],["8S","QC","AC","TD"]],"held":[5,4],"stock":["8D","KH","9S","8H","QS","TS","9D","KS","QH","7C","7H","AD","JC","KC","AS","JS","QD","7S","TH","7D","JH","9H","TC","JD"],"left":24,"trump":"JD","table":[{"attack":"6H","defense":"AH"},{"attack":"6D"}],"discard":[],"attacker":1,"limit":6,"taking":true,"moves":["attack 6H","defend 6H AH","attack 6D","take"]}}`

func TestLegacyStateStillPlays(t *testing.T) {
	var state engine.State
	if err := json.Unmarshal([]byte(legacyState), &state); err != nil {
		t.Fatal(err)
	}
	d, p := full(t, state)
	if d.Options != nil || p.Options != DefaultOptions(2) || len(p.Hands) != 2 || !p.Taking || p.Table[0].By != 1 || p.Table[1].By != 1 {
		t.Fatalf("legacy position %+v", p)
	}
	if got := notation(New().ValidMoves(state, "south")); !slices.Equal(got, []string{"attack AC", "pass"}) {
		t.Fatal("legacy throw-ins", got)
	}
	if got := New().ValidMoves(state, "north"); got != nil {
		t.Fatal("the taker acts", notation(got))
	}
	snap, err := Describe(New().View(state, "north"))
	if err != nil || len(snap.Hands) != 2 || !snap.Hands[1].Hidden || snap.Hands[1].Count != 4 || snap.Defender != 0 || snap.Options != DefaultOptions(2) || len(snap.Table) != 2 {
		t.Fatalf("legacy snapshot %+v, %v", snap, err)
	}
	state = apply(t, state, "attack AC")
	state = apply(t, state, "pass")
	d, p = full(t, state)
	if state.Turn != 1 || p.Attacker != 1 || !slices.Equal(d.Held, []int{9, 6}) || d.Left != 21 || len(d.Moves) != 6 || d.Options == nil {
		t.Fatalf("after the legacy take: turn %d held %v left %d moves %v", state.Turn, d.Held, d.Left, d.Moves)
	}
	finished := engine.State{Game: Key, Players: []engine.PlayerID{"north", "south"}, Status: engine.StatusWin, Winner: 0, Turn: engine.NoSeat,
		Data: json.RawMessage(`{"hands":[[],["6S"]],"held":[0,1],"stock":[],"left":0,"trump":"JD","table":[],"discard":[],"attacker":0,"limit":1,"taking":false,"moves":["pass"],"end":"durak"}`)}
	if snap, err := Describe(finished); err != nil || !slices.Equal(snap.Places, []int{1, 2}) || snap.End != Lost {
		t.Fatalf("legacy finished game places %v, %v", snap.Places, err)
	}
}

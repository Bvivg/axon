package session_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

func durakTable(t *testing.T, seats, bots int, cheats bool) session.Table {
	t.Helper()
	options := durak.DefaultOptions(seats)
	options.Cheats = cheats
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	return session.Table{Seats: seats, Options: raw, Bots: bots}
}

func playBots(t *testing.T, r session.Rules, game session.Session, now time.Time) session.Session {
	t.Helper()
	for step := 0; step < 2000 && !game.State.Over(); step++ {
		next, change, err := r.Bot(game, now)
		if err != nil {
			t.Fatalf("bot step %d: %v", step, err)
		}
		if !change.Recorded() {
			t.Fatalf("bot step %d: no bot could move at turn %d", step, game.State.Turn)
		}
		game = session.Record(next, change)
	}
	if !game.State.Over() {
		t.Fatal("bots did not finish the game")
	}
	return game
}

func TestTableSeatsPlayersAndDealsWhenFull(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	host, second, third := uuid.New(), uuid.New(), uuid.New()
	game, change, err := r.Create(host, uuid.New(), durak.Key, durakTable(t, 3, 0, false), now)
	if err != nil || change.Fact != session.FactCreated {
		t.Fatal(change, err)
	}
	if game.Started() || game.Capacity() != 3 || !slices.Equal(game.Players(), []uuid.UUID{host}) {
		t.Fatalf("fresh table: started=%v capacity=%d players=%v", game.Started(), game.Capacity(), game.Players())
	}
	if game, _, err = r.Join(game, second, now); err != nil || game.Started() {
		t.Fatal("second seat", game.Started(), err)
	}
	if again, change, err := r.Join(game, second, now); err != nil || change.Recorded() || len(again.Players()) != 2 {
		t.Fatal("joining twice changed the table", change, err)
	}
	if game, _, err = r.Join(game, third, now); err != nil || !game.Started() {
		t.Fatal("third seat", err)
	}
	if len(game.State.Players) != 3 || len(game.Clock.RemainingMS) != 3 || *game.Opponent != second {
		t.Fatalf("deal: players=%d clocks=%d", len(game.State.Players), len(game.Clock.RemainingMS))
	}
	if _, _, err := r.Join(game, uuid.New(), now); !errors.Is(err, session.ErrFull) {
		t.Fatal("fourth player joined a full table", err)
	}
}

func TestWaitingPlayerLeavesTheTableWithoutCancellingIt(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	host, guest := uuid.New(), uuid.New()
	game, _, _ := r.Create(host, uuid.New(), durak.Key, durakTable(t, 4, 0, false), now)
	game, _, _ = r.Join(game, guest, now)
	left, change, err := r.Leave(game, guest, now)
	if err != nil || change.Fact != session.FactLeft || !slices.Equal(left.Players(), []uuid.UUID{host}) {
		t.Fatal(change, left.Players(), err)
	}
	if len(game.Players()) != 2 {
		t.Fatal("leaving mutated the previous table")
	}
	if _, change, _ := r.Leave(left, host, now); change.Fact != session.FactCancelled {
		t.Fatal("host leaving must cancel the table", change)
	}
}

func TestInvalidTablesAreRejected(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	cases := map[string]struct {
		key   engine.Key
		table session.Table
	}{
		"too many seats":     {durak.Key, durakTable(t, 7, 0, false)},
		"bots fill all":      {durak.Key, durakTable(t, 3, 3, false)},
		"deck too small":     {durak.Key, session.Table{Seats: 5, Options: json.RawMessage(`{"mode":"throw-in","deck":24,"throw_in":"all","draw":true,"first_bout_limit":6,"seats":5}`)}},
		"options elsewhere":  {tictactoe.Key, durakTable(t, 2, 0, false)},
		"tictactoe for 3":    {tictactoe.Key, session.Table{Seats: 3}},
		"bots in tictactoe":  {tictactoe.Key, session.Table{Seats: 2, Bots: 1}},
		"mismatched options": {durak.Key, session.Table{Seats: 3, Options: durakTable(t, 4, 0, false).Options}},
	}
	for name, tc := range cases {
		if _, _, err := r.Create(uuid.New(), uuid.New(), tc.key, tc.table, now); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBotsFinishATableAndPlacesAreComplete(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	for seats := 2; seats <= 6; seats++ {
		for _, cheats := range []bool{false, true} {
			host := uuid.New()
			game, _, err := r.Create(host, uuid.New(), durak.Key, durakTable(t, seats, seats-1, cheats), now)
			if err != nil || !game.Started() {
				t.Fatalf("%d seats: %v", seats, err)
			}
			if !game.Automated(1) || game.Automated(0) || !session.IsBot(game.Players()[seats-1]) {
				t.Fatalf("%d seats: bot seats are wrong", seats)
			}
			game.Left = []int{0}
			game = playBots(t, r, game, now)
			places := game.Places()
			if len(places) != seats || places[0] != seats {
				t.Fatalf("%d seats: places %v, the leaver must be last", seats, places)
			}
			for seat, place := range places {
				if place < 1 || place > seats {
					t.Fatalf("%d seats: seat %d place %d", seats, seat, place)
				}
			}
		}
	}
}

func TestFillWithBotsStartsTheTableForTheHostOnly(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	host, guest := uuid.New(), uuid.New()
	game, _, _ := r.Create(host, uuid.New(), durak.Key, durakTable(t, 4, 0, false), now)
	game, _, _ = r.Join(game, guest, now)
	if _, _, err := r.FillWithBots(game, guest, now); !errors.Is(err, session.ErrNotHost) {
		t.Fatal("a guest filled the table", err)
	}
	full, change, err := r.FillWithBots(game, host, now)
	if err != nil || change.Fact != session.FactJoined || !full.Started() {
		t.Fatal(change, err)
	}
	players := full.Players()
	if players[0] != host || players[1] != guest || !session.IsBot(players[2]) || !session.IsBot(players[3]) || players[2] == players[3] {
		t.Fatalf("seating %v", players)
	}
	if _, _, err := r.FillWithBots(game, uuid.New(), now); !errors.Is(err, session.ErrNotFound) {
		t.Fatal("a stranger filled the table", err)
	}
}

func TestLeaverAtAFullTableIsReplacedAndRanksLast(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	game, _, err := r.Start(uuid.New(), durak.Key, []uuid.UUID{a, b, c}, durakTable(t, 3, 0, false), true, now)
	if err != nil {
		t.Fatal(err)
	}
	left, change, err := r.Leave(game, b, now)
	if err != nil || change.Fact != session.FactLeft || !slices.Equal(left.Left, []int{1}) || left.State.Over() {
		t.Fatal(change, left.Left, err)
	}
	if _, _, err := r.Move(left, b, left.State.Ply, durak.Key, json.RawMessage(`{"move":"take"}`), nil, now); !errors.Is(err, session.ErrNotPlaying) {
		t.Fatal("a leaver moved", err)
	}
	if again, change, _ := r.Leave(left, b, now); change.Recorded() || len(again.Left) != 1 {
		t.Fatal("leaving twice", change)
	}
	if _, _, err := r.Draw(left, a, session.OfferDraw, now); !errors.Is(err, session.ErrTwoPlayers) {
		t.Fatal("draw offer at a three-seat table", err)
	}
	left.Left = []int{1, 0, 2}
	done := playBots(t, r, left, now)
	if places := done.Places(); !slices.Equal(places, []int{2, 3, 1}) {
		t.Fatalf("places %v", places)
	}
}

func TestTimeoutAtAFullTableHandsTheSeatToABot(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	players := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	game, _, err := r.Start(uuid.New(), durak.Key, players, durakTable(t, 3, 0, false), true, now)
	if err != nil {
		t.Fatal(err)
	}
	turn := game.State.Turn
	late := now.Add(time.Duration(game.Clock.RemainingMS[turn]+1) * time.Millisecond)
	expired, change := r.Expire(game, late)
	if change.Fact != session.FactTimedOut || expired.State.Over() || !slices.Equal(expired.Left, []int{turn}) || expired.TimedOut {
		t.Fatal(change, expired.Left)
	}
	if again, change := r.Expire(expired, late.Add(time.Hour)); change.Recorded() && slices.Contains(again.Left[1:], turn) {
		t.Fatal("an automated seat timed out twice")
	}
	if player, _, ok := r.BotChoice(expired); !ok || player != players[turn] {
		t.Fatal("no bot plays for the expired seat", ok)
	}
}

func TestPlainGamesKeepTwoSeatBehaviour(t *testing.T) {
	r := cardRules(t)
	now := time.Now()
	host, guest := uuid.New(), uuid.New()
	game, _, err := r.Create(host, uuid.New(), tictactoe.Key, session.Table{}, now)
	if err != nil || game.Seats != 0 || game.Seated != nil {
		t.Fatal(game.Seats, game.Seated, err)
	}
	game, _, err = r.Join(game, guest, now)
	if err != nil || !game.Started() || game.Seated != nil || len(game.Clock.RemainingMS) != 2 {
		t.Fatal(game.Seated, err)
	}
	if _, _, ok := r.BotChoice(game); ok {
		t.Fatal("a bot moves in a game without bots")
	}
	left, change, err := r.Leave(game, guest, now)
	if err != nil || change.Fact != session.FactResigned || left.State.Winner != 0 {
		t.Fatal(change, err)
	}
	if places := left.Places(); !slices.Equal(places, []int{1, 2}) {
		t.Fatal(places)
	}
}

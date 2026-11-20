package saga_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

var epoch = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func users(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1))
	}
	return out
}

func open(t *testing.T, spec saga.Spec, players []uuid.UUID, ratings ...int) saga.Tournament {
	t.Helper()
	tour, err := saga.Create(uuid.MustParse("7d8e9f00-1111-4222-8333-444455556666"), players[0], spec, epoch)
	if err != nil {
		t.Fatal(err)
	}
	for i, user := range players {
		rating := 1500
		if i < len(ratings) {
			rating = ratings[i]
		}
		var changed bool
		tour, changed, err = tour.Register(user, rating, epoch.Add(time.Duration(i)*time.Second))
		if err != nil || !changed {
			t.Fatal(i, changed, err)
		}
	}
	return tour
}

func start(t *testing.T, tour saga.Tournament) (saga.Tournament, saga.RoundStarted) {
	t.Helper()
	tour, changed, err := tour.Start(epoch.Add(time.Minute))
	if err != nil || !changed || tour.Status != saga.Seeding {
		t.Fatal(changed, err, tour.Status)
	}
	return advance(t, tour, epoch.Add(time.Minute))
}

func advance(t *testing.T, tour saga.Tournament, now time.Time) (saga.Tournament, saga.RoundStarted) {
	t.Helper()
	next, effects, err := tour.Advance(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 1 {
		t.Fatalf("advance produced %d effects", len(effects))
	}
	started, ok := effects[0].(saga.RoundStarted)
	if !ok {
		t.Fatalf("advance produced %T", effects[0])
	}
	return next, started
}

func play(t *testing.T, tour saga.Tournament, m saga.Match, winner, plies int) saga.Tournament {
	t.Helper()
	next, changed, err := tour.Record(m.Session, saga.Outcome{Players: m.Seats, Winner: winner, Plies: plies, At: epoch.Add(time.Hour)})
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	return next
}

var elimination = saga.Spec{Name: "Friday cup", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 8}

func TestValidSpec(t *testing.T) {
	for _, tt := range []struct {
		name string
		spec saga.Spec
		ok   bool
	}{
		{"bracket of 8", elimination, true},
		{"bracket of 16", saga.Spec{Name: "x", Game: "chess", Format: saga.SingleElimination, Capacity: 16}, true},
		{"bracket of 6", saga.Spec{Name: "x", Game: "chess", Format: saga.SingleElimination, Capacity: 6}, false},
		{"swiss of 10 in 4 rounds", saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 10, Rounds: 4}, true},
		{"swiss without rounds", saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 10}, false},
		{"swiss with too many rounds", saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 4, Rounds: 4}, false},
		{"swiss too large", saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 65, Rounds: 3}, false},
		{"blank name", saga.Spec{Name: "   ", Game: "chess", Format: saga.SingleElimination, Capacity: 4}, false},
		{"long name", saga.Spec{Name: string(make([]rune, 65)), Game: "chess", Format: saga.SingleElimination, Capacity: 4}, false},
		{"no game", saga.Spec{Name: "x", Format: saga.SingleElimination, Capacity: 4}, false},
		{"unknown format", saga.Spec{Name: "x", Game: "chess", Format: "round_robin", Capacity: 4}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := saga.ValidSpec(tt.spec)
			if (err == nil) != tt.ok || err != nil && !errors.Is(err, saga.ErrInvalid) {
				t.Fatalf("ValidSpec = %v, want ok %v", err, tt.ok)
			}
		})
	}
	trimmed, err := saga.ValidSpec(saga.Spec{Name: "  Cup  ", Game: "chess", Format: saga.SingleElimination, Capacity: 4, Rounds: 7})
	if err != nil || trimmed.Name != "Cup" || trimmed.Rounds != 0 {
		t.Fatalf("trimmed = %+v, %v", trimmed, err)
	}
}

func TestRegistrationRules(t *testing.T) {
	ids := users(5)
	tour := open(t, saga.Spec{Name: "x", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, ids[:4])
	if _, _, err := tour.Register(ids[4], 1500, epoch); !errors.Is(err, saga.ErrFull) {
		t.Fatalf("fifth registration = %v", err)
	}
	if again, changed, err := tour.Register(ids[1], 1700, epoch); err != nil || changed || len(again.Players) != 4 {
		t.Fatalf("repeat registration = %v %v", changed, err)
	}
	tour, changed, err := tour.Withdraw(ids[3])
	if err != nil || !changed || tour.Has(ids[3]) || len(tour.Registered()) != 3 {
		t.Fatalf("withdraw = %v %v", changed, err)
	}
	if _, changed, err := tour.Withdraw(ids[3]); err != nil || changed {
		t.Fatalf("repeat withdraw = %v %v", changed, err)
	}
	started, _ := start(t, tour)
	if _, _, err := started.Register(ids[4], 1500, epoch); !errors.Is(err, saga.ErrClosed) {
		t.Fatalf("late registration = %v", err)
	}
	if _, _, err := started.Withdraw(ids[0]); !errors.Is(err, saga.ErrClosed) {
		t.Fatalf("late withdraw = %v", err)
	}
	lonely := open(t, elimination, ids[:1])
	if _, _, err := lonely.Start(epoch); !errors.Is(err, saga.ErrTooFew) {
		t.Fatalf("start with one player = %v", err)
	}
}

func TestSeedingOrdersByRatingThenRegistrationAndHandsOutByes(t *testing.T) {
	ids := users(5)
	tour := open(t, elimination, ids, 1500, 1500, 1800, 1500, 1600)
	tour, started := start(t, tour)
	seeds := map[uuid.UUID]int{}
	for _, p := range tour.Players {
		seeds[p.User] = p.Seed
	}
	if want := map[uuid.UUID]int{ids[2]: 1, ids[4]: 2, ids[0]: 3, ids[1]: 4, ids[3]: 5}; fmt.Sprint(seeds) != fmt.Sprint(want) {
		t.Fatalf("seeds = %v, want %v", seeds, want)
	}
	if tour.Status != saga.Running || tour.Round != 1 || tour.TotalRounds != 3 || len(tour.Matches) != 4 {
		t.Fatalf("tournament = %+v", tour)
	}
	byes := 0
	for _, m := range tour.Matches {
		if m.Bye() {
			byes++
			if m.Result != saga.Bye || m.Winner != m.Seats[0] || m.Session != uuid.Nil || m.FinishedAt == nil {
				t.Fatalf("bye = %+v", m)
			}
		}
	}
	if byes != 3 || len(started.Matches) != 1 {
		t.Fatalf("%d byes, round_started carries %d games", byes, len(started.Matches))
	}
	game := started.Matches[0]
	if game.Seats != [2]uuid.UUID{ids[1], ids[3]} || game.Session != tournamentid.Session(tour.ID, 1, 2) || game.Board != 2 {
		t.Fatalf("the only real game = %+v", game)
	}
	if _, effects, err := tour.Advance(epoch); err != nil || len(effects) != 0 {
		t.Fatalf("advance of an unfinished round = %v %v", effects, err)
	}
}

func TestEightPlayerBracketReachesAChampion(t *testing.T) {
	ids := users(8)
	tour, started := start(t, open(t, elimination, ids))
	var finished *saga.Completion
	for round := 1; finished == nil; round++ {
		if started.Round != round || len(started.Matches) != 8>>round {
			t.Fatalf("round %d has %d games", started.Round, len(started.Matches))
		}
		for _, m := range started.Matches {
			plies := 5
			if m.Seats[1] == ids[7] {
				plies = 1
			}
			tour = play(t, tour, m, 0, plies)
		}
		current, _ := tour.CurrentRound()
		if !tour.RoundComplete() || current.FinishedAt == nil {
			t.Fatalf("round %d is not complete", round)
		}
		next, effects, err := tour.Advance(epoch.Add(2 * time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		tour = next
		switch effect := effects[0].(type) {
		case saga.RoundStarted:
			started = effect
		case saga.Completion:
			finished = &effect
		}
	}
	if tour.Status != saga.Finished || tour.Winner != ids[0] || finished.Winner != ids[0] || tour.FinishedAt == nil {
		t.Fatalf("finished = %+v, winner %s", tour.Status, tour.Winner)
	}
	var ranks []int
	var order []uuid.UUID
	for _, s := range finished.Standings {
		ranks, order = append(ranks, s.Rank), append(order, s.User)
	}
	if !slices.Equal(ranks, []int{1, 2, 3, 3, 5, 5, 5, 5}) || order[0] != ids[0] || order[1] != ids[1] {
		t.Fatalf("ranks %v, order %v", ranks, order)
	}
	forfeit, _ := tour.Match(tournamentid.Session(tour.ID, 1, 1))
	if !forfeit.Forfeit || forfeit.Winner != ids[0] {
		t.Fatalf("the no-show board = %+v", forfeit)
	}
	if again, changed, err := tour.Record(forfeit.Session, saga.Outcome{Players: forfeit.Seats, Winner: 1}); !errors.Is(err, saga.ErrNotRunning) || changed || again.Winner != ids[0] {
		t.Fatalf("a late result = %v %v", changed, err)
	}
	if _, _, err := tour.Cancel(epoch); !errors.Is(err, saga.ErrFinal) {
		t.Fatalf("cancel after the final = %v", err)
	}
}

func TestBracketDrawSendsTheBetterSeedThrough(t *testing.T) {
	ids := users(4)
	tour, started := start(t, open(t, saga.Spec{Name: "x", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, ids))
	tour = play(t, tour, started.Matches[0], saga.NoWinner, 9)
	tour = play(t, tour, started.Matches[1], 1, 6)
	drawn, _ := tour.Match(started.Matches[0].Session)
	if drawn.Result != saga.Draw || drawn.Winner != ids[0] {
		t.Fatalf("drawn board = %+v", drawn)
	}
	tour, final := advance(t, tour, epoch.Add(2*time.Hour))
	if final.Matches[0].Seats != [2]uuid.UUID{ids[0], ids[2]} {
		t.Fatalf("final = %+v", final.Matches[0].Seats)
	}
	standings := tour.Standings()
	if standings[0].Draws != 1 || standings[0].HalfPoints != saga.DrawHalfPoints {
		t.Fatalf("standings = %+v", standings)
	}
}

func TestRecordRejectsForeignResults(t *testing.T) {
	ids := users(4)
	tour, started := start(t, open(t, saga.Spec{Name: "x", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, ids))
	m := started.Matches[0]
	if _, _, err := tour.Record(uuid.New(), saga.Outcome{Players: m.Seats, Winner: 0}); !errors.Is(err, saga.ErrUnknownMatch) {
		t.Fatalf("unknown session = %v", err)
	}
	if _, _, err := tour.Record(m.Session, saga.Outcome{Players: [2]uuid.UUID{m.Seats[1], m.Seats[0]}, Winner: 0}); !errors.Is(err, saga.ErrMismatch) {
		t.Fatalf("swapped seats = %v", err)
	}
	if _, _, err := tour.Record(m.Session, saga.Outcome{Players: m.Seats, Winner: 3}); !errors.Is(err, saga.ErrMismatch) {
		t.Fatalf("bad winner seat = %v", err)
	}
	cancelled, _, err := tour.Cancel(epoch)
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := cancelled.Record(m.Session, saga.Outcome{Players: m.Seats, Winner: 0}); !errors.Is(err, saga.ErrNotRunning) || changed {
		t.Fatalf("result after the cancellation = %v %v", changed, err)
	}
	if _, changed, err := tour.Record(m.Session, saga.Outcome{Players: m.Seats, Winner: 0}); err != nil || !changed {
		t.Fatalf("first result = %v %v", changed, err)
	}
}

func TestForfeitOnlyForPendingGamesOfARunningTournament(t *testing.T) {
	ids := users(4)
	tour, started := start(t, open(t, saga.Spec{Name: "x", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}, ids))
	pending, decided := started.Matches[0], started.Matches[1]
	tour = play(t, tour, decided, 0, 5)
	due := epoch.Add(3 * time.Minute)
	effect, ok := tour.Forfeit(pending.Session, due)
	issued, isForfeit := effect.(saga.ForfeitIssued)
	if !ok || !isForfeit || issued.Match.Session != pending.Session || issued.Game != "tictactoe" || !issued.Due.Equal(due) {
		t.Fatalf("forfeit = %+v %v", effect, ok)
	}
	for name, session := range map[string]uuid.UUID{"decided": decided.Session, "unknown": uuid.New()} {
		if _, ok := tour.Forfeit(session, due); ok {
			t.Fatalf("forfeit of a %s game", name)
		}
	}
	cancelled, _, _ := tour.Cancel(epoch)
	if _, ok := cancelled.Forfeit(pending.Session, due); ok {
		t.Fatal("forfeit in a cancelled tournament")
	}
}

func TestCancelCompensations(t *testing.T) {
	ids := users(4)
	spec := saga.Spec{Name: "x", Game: "tictactoe", Format: saga.SingleElimination, Capacity: 4}
	before := open(t, spec, ids)
	cancelled, effect, err := before.Cancel(epoch)
	if err != nil {
		t.Fatal(err)
	}
	c := effect.(saga.Cancellation)
	if cancelled.Status != saga.Cancelled || c.Started || len(c.Returned) != 4 || len(c.Sessions) != 0 || len(cancelled.Registered()) != 0 {
		t.Fatalf("cancel before the start = %+v %+v", cancelled.Status, c)
	}
	if _, _, err := cancelled.Start(epoch); !errors.Is(err, saga.ErrFinal) {
		t.Fatalf("start after cancel = %v", err)
	}
	tour, started := start(t, open(t, spec, ids))
	tour = play(t, tour, started.Matches[0], 0, 5)
	cancelled, effect, err = tour.Cancel(epoch.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c = effect.(saga.Cancellation)
	if !c.Started || len(c.Returned) != 0 || !slices.Equal(c.Sessions, []uuid.UUID{started.Matches[1].Session}) {
		t.Fatalf("cancel during play = %+v", c)
	}
	aborted, _ := cancelled.Match(started.Matches[1].Session)
	kept, _ := cancelled.Match(started.Matches[0].Session)
	if aborted.Result != saga.Aborted || kept.Result != saga.Win || len(cancelled.Registered()) != 4 {
		t.Fatalf("matches = %+v %+v", aborted, kept)
	}
	if _, effects, err := cancelled.Advance(epoch); err != nil || len(effects) != 0 {
		t.Fatalf("advance after the cancellation = %v %v", effects, err)
	}
}

func TestSwissRotatesByesAndAvoidsRematches(t *testing.T) {
	ids := users(5)
	spec := saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 8, Rounds: 4}
	tour, started := start(t, open(t, spec, ids))
	if tour.TotalRounds != 4 {
		t.Fatalf("total rounds = %d", tour.TotalRounds)
	}
	met := map[[2]uuid.UUID]bool{}
	byes := map[uuid.UUID]int{}
	var completion saga.Completion
	for round := 1; ; round++ {
		for _, m := range tour.RoundMatches(round) {
			if m.Bye() {
				byes[m.Seats[0]]++
				continue
			}
			if met[m.Seats] || met[[2]uuid.UUID{m.Seats[1], m.Seats[0]}] {
				t.Fatalf("round %d repeats %v", round, m.Seats)
			}
			met[m.Seats] = true
		}
		for i, m := range started.Matches {
			winner := 0
			if i == 0 && round == 2 {
				winner = saga.NoWinner
			}
			tour = play(t, tour, m, winner, 10)
		}
		next, effects, err := tour.Advance(epoch.Add(time.Duration(round) * time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		tour = next
		if done, ok := effects[0].(saga.Completion); ok {
			completion = done
			break
		}
		started = effects[0].(saga.RoundStarted)
	}
	for user, n := range byes {
		if n > 1 {
			t.Fatalf("%s had %d byes", user, n)
		}
	}
	if len(byes) != 4 || tour.Round != 4 {
		t.Fatalf("byes %v after %d rounds", byes, tour.Round)
	}
	total := 0
	for i, s := range completion.Standings {
		total += s.HalfPoints
		if s.Rank != i+1 || i > 0 && s.HalfPoints > completion.Standings[i-1].HalfPoints {
			t.Fatalf("standings out of order: %+v", completion.Standings)
		}
	}
	if total != 4*3*saga.WinHalfPoints || completion.Winner != completion.Standings[0].User || tour.Winner != completion.Winner {
		t.Fatalf("total half points %d, winner %s", total, completion.Winner)
	}
}

func TestSwissRoundsAreCappedByThePlayers(t *testing.T) {
	ids := users(3)
	tour, _ := start(t, open(t, saga.Spec{Name: "x", Game: "chess", Format: saga.Swiss, Capacity: 8, Rounds: 5}, ids))
	if tour.TotalRounds != 2 {
		t.Fatalf("total rounds = %d, want 2", tour.TotalRounds)
	}
}

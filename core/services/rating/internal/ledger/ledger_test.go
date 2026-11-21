package ledger_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

var (
	alice    = uuid.MustParse("1d6f2f43-9b35-4a6f-8f43-2a1c9d0e7b11")
	bob      = uuid.MustParse("2e7a3b54-ac46-4b70-9054-3b2dae1f8c22")
	carol    = uuid.MustParse("3f8b4c65-bd57-4c81-a165-4c3ebf209d33")
	finished = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
)

func event(winner int32, outcome eventsv1.GameFinished_Outcome) *eventsv1.GameFinished {
	return &eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: "chess", Players: []string{alice.String(), bob.String()},
		Outcome: outcome, WinnerSeat: winner, FinishedAt: timestamppb.New(finished), Rated: true,
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*eventsv1.GameFinished)
		winner int
		err    error
	}{
		{name: "creator wins", mutate: func(*eventsv1.GameFinished) {}, winner: 0},
		{name: "opponent wins", mutate: func(e *eventsv1.GameFinished) { e.WinnerSeat = 1 }, winner: 1},
		{name: "draw ignores the seat", mutate: func(e *eventsv1.GameFinished) { e.Outcome, e.WinnerSeat = eventsv1.GameFinished_OUTCOME_DRAW, 7 }, winner: -1},
		{name: "unrated", mutate: func(e *eventsv1.GameFinished) { e.Rated = false }, err: ledger.ErrUnrated},
		{name: "bad event id", mutate: func(e *eventsv1.GameFinished) { e.EventId = "x" }, err: ledger.ErrInvalid},
		{name: "bad session", mutate: func(e *eventsv1.GameFinished) { e.SessionId = "" }, err: ledger.ErrInvalid},
		{name: "bad game key", mutate: func(e *eventsv1.GameFinished) { e.Game = "Chess!" }, err: ledger.ErrInvalid},
		{name: "one player", mutate: func(e *eventsv1.GameFinished) { e.Players = e.Players[:1] }, err: ledger.ErrInvalid},
		{name: "bad player", mutate: func(e *eventsv1.GameFinished) { e.Players[1] = "x" }, err: ledger.ErrInvalid},
		{name: "self play", mutate: func(e *eventsv1.GameFinished) { e.Players[1] = alice.String() }, err: ledger.ErrInvalid},
		{name: "winner seat out of range", mutate: func(e *eventsv1.GameFinished) { e.WinnerSeat = 2 }, err: ledger.ErrInvalid},
		{name: "unknown outcome", mutate: func(e *eventsv1.GameFinished) { e.Outcome = eventsv1.GameFinished_OUTCOME_UNSPECIFIED }, err: ledger.ErrInvalid},
		{name: "no finish time", mutate: func(e *eventsv1.GameFinished) { e.FinishedAt = nil }, err: ledger.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := event(0, eventsv1.GameFinished_OUTCOME_WIN)
			tt.mutate(raw)
			game, err := ledger.Parse(raw)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("Parse() error = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			if game.Winner != tt.winner || !slices.Equal(game.Players, []uuid.UUID{alice, bob}) || game.Game != "chess" || !game.FinishedAt.Equal(finished) {
				t.Fatalf("Parse() = %+v", game)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	raw, err := proto.Marshal(event(1, eventsv1.GameFinished_OUTCOME_WIN))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		kind  string
		value []byte
		err   error
	}{
		{name: "typed", kind: "axon.events.v1.GameFinished", value: raw},
		{name: "untyped", value: raw},
		{name: "another event", kind: "axon.events.v1.UserRegistered", value: raw, err: ledger.ErrForeign},
		{name: "garbage", value: []byte{0xff, 0x01}, err: ledger.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			game, err := ledger.Decode(tt.kind, tt.value)
			if !errors.Is(err, tt.err) || (tt.err == nil) != (err == nil) {
				t.Fatalf("Decode() error = %v, want %v", err, tt.err)
			}
			if err == nil && game.Winner != 1 {
				t.Fatalf("Decode() = %+v", game)
			}
		})
	}
}

func TestSettle(t *testing.T) {
	players := []ledger.Player{ledger.Newcomer(alice), ledger.Newcomer(bob)}
	tests := []struct {
		name         string
		winner       int
		wins, losses [2]int
		draws        int
		risesFirst   bool
		equalRatings bool
	}{
		{name: "creator wins", winner: 0, wins: [2]int{1, 0}, losses: [2]int{0, 1}, risesFirst: true},
		{name: "opponent wins", winner: 1, wins: [2]int{0, 1}, losses: [2]int{1, 0}},
		{name: "draw", winner: -1, draws: 1, equalRatings: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after := ledger.Settle(glicko.Default, ledger.Game{Winner: tt.winner, Places: duel(tt.winner)}, players)
			for seat, player := range after {
				if player.User != players[seat].User || player.Games != 1 || player.Wins != tt.wins[seat] || player.Losses != tt.losses[seat] || player.Draws != tt.draws {
					t.Fatalf("seat %d = %+v", seat, player)
				}
			}
			switch {
			case tt.equalRatings && after[0].Rating != after[1].Rating:
				t.Fatalf("draw between equals diverged: %+v", after)
			case !tt.equalRatings && (after[0].Rating.Rating > after[1].Rating.Rating) != tt.risesFirst:
				t.Fatalf("the winner did not gain: %+v", after)
			}
		})
	}
}

func parsed(t *testing.T, raw *eventsv1.GameFinished) ledger.Game {
	t.Helper()
	game, err := ledger.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return game
}

func TestBookAppliesEachEventAndSessionOnce(t *testing.T) {
	book := ledger.NewBook(glicko.Default)
	first := parsed(t, event(0, eventsv1.GameFinished_OUTCOME_WIN))
	if !book.Apply(first) {
		t.Fatal("first game was not applied")
	}
	if book.Apply(first) {
		t.Fatal("a redelivered event was applied twice")
	}
	replayed := first
	replayed.EventID = uuid.New()
	if book.Apply(replayed) {
		t.Fatal("a second event for the same session was applied")
	}
	second := parsed(t, event(1, eventsv1.GameFinished_OUTCOME_WIN))
	second.Players = []uuid.UUID{alice, carol}
	if !book.Apply(second) {
		t.Fatal("second game was not applied")
	}
	winner, _ := glicko.Default.Game(glicko.Newcomer, glicko.Newcomer, glicko.Win)
	expected, _ := glicko.Default.Game(winner, glicko.Newcomer, glicko.Loss)
	if got := book.Players[ledger.Key{Game: "chess", User: alice}]; got.Rating != expected || got.Games != 2 || got.Wins != 1 || got.Losses != 1 {
		t.Fatalf("alice = %+v, want rating %+v after a win and a loss", got, expected)
	}
	if len(book.Events) != 3 || len(book.Changes) != 4 || len(book.Standings("chess")) != 3 || len(book.Games()) != 1 {
		t.Fatalf("book = %d events, %d changes, %d players", len(book.Events), len(book.Changes), len(book.Standings("chess")))
	}
	if book.Changes[2].Before.Rating != winner || book.Changes[2].After.Rating != expected {
		t.Fatalf("change chain broken: %+v", book.Changes[2])
	}
}

func TestAnnouncement(t *testing.T) {
	game := parsed(t, event(0, eventsv1.GameFinished_OUTCOME_WIN))
	before := []ledger.Player{ledger.Newcomer(alice), ledger.Newcomer(bob)}
	after := ledger.Settle(glicko.Default, game, before)
	announced := ledger.Announcement(game, before, after)
	payload, ok := announced.Payload.(*eventsv1.RatingChanged)
	if !ok || announced.Topic != "rating.changed.v1" || announced.AggregateID != "chess" || announced.ID != ledger.ChangedEventID(game.EventID) {
		t.Fatalf("envelope = %+v", announced)
	}
	if announced.ID == ledger.ChangedEventID(uuid.New()) || payload.EventId != announced.ID.String() || payload.SourceEventId != game.EventID.String() ||
		payload.SessionId != game.Session.String() || !payload.ChangedAt.AsTime().Equal(finished) {
		t.Fatalf("payload = %v", payload)
	}
	want := []struct {
		user          uuid.UUID
		before, after int32
		wins, losses  int32
	}{{alice, 1200, 1362, 1, 0}, {bob, 1200, 1038, 0, 1}}
	for seat, change := range payload.Changes {
		w := want[seat]
		if change.UserId != w.user.String() || change.Before != w.before || change.After != w.after || change.Games != 1 ||
			change.Wins != w.wins || change.Losses != w.losses || change.Deviation != after[seat].Rating.Deviation {
			t.Fatalf("change %d = %v", seat, change)
		}
	}
}

func duel(winner int) []int {
	switch winner {
	case 0:
		return []int{1, 2}
	case 1:
		return []int{2, 1}
	default:
		return []int{1, 1}
	}
}

func TestSettleRanksATableByPlaces(t *testing.T) {
	users := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	before := make([]ledger.Player, len(users))
	for seat, user := range users {
		before[seat] = ledger.Newcomer(user)
	}
	after := ledger.Settle(glicko.Default, ledger.Game{Players: users, Places: []int{3, 1, 4, 2}, Winner: 1}, before)
	gain := func(seat int) float64 { return after[seat].Rating.Rating - before[seat].Rating.Rating }
	if !(gain(1) > gain(3) && gain(3) > 0 && gain(0) < 0 && gain(2) < gain(0)) {
		t.Fatalf("gains by place 1..4: %.1f %.1f %.1f %.1f", gain(1), gain(3), gain(0), gain(2))
	}
	if after[1].Wins != 1 || after[2].Losses != 1 || after[0].Draws != 1 || after[3].Draws != 1 {
		t.Fatalf("counters %+v", after)
	}
}

func TestParseTablePlaces(t *testing.T) {
	users := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	base := func() *eventsv1.GameFinished {
		return &eventsv1.GameFinished{EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: "durak", Players: users,
			Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 2, Rated: true, FinishedAt: timestamppb.New(finished), Places: []uint32{2, 3, 1}}
	}
	game, err := ledger.Parse(base())
	if err != nil || !slices.Equal(game.Places, []int{2, 3, 1}) || len(game.Players) != 3 {
		t.Fatal(game, err)
	}
	missing := base()
	missing.Places = nil
	if _, err := ledger.Parse(missing); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal("three players without places", err)
	}
	wrong := base()
	wrong.Places = []uint32{1, 4, 2}
	if _, err := ledger.Parse(wrong); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal("place out of range", err)
	}
	twice := base()
	twice.Players = []string{users[0], users[1], users[0]}
	if _, err := ledger.Parse(twice); !errors.Is(err, ledger.ErrInvalid) {
		t.Fatal("duplicate player", err)
	}
}

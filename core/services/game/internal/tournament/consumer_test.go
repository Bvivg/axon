package tournament_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/tournament"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

type fakeGames struct {
	started   map[uuid.UUID]tournament.Game
	forfeited map[uuid.UUID]int
	aborted   map[uuid.UUID]int
	fail      error
}

func newGames() *fakeGames {
	return &fakeGames{started: map[uuid.UUID]tournament.Game{}, forfeited: map[uuid.UUID]int{}, aborted: map[uuid.UUID]int{}}
}

func (f *fakeGames) StartTournamentGame(_ context.Context, game tournament.Game) error {
	if f.fail != nil {
		return f.fail
	}
	f.started[game.Session] = game
	return nil
}

func (f *fakeGames) ForfeitTournamentGame(_ context.Context, game tournament.Game) error {
	if f.fail != nil {
		return f.fail
	}
	f.started[game.Session] = game
	f.forfeited[game.Session]++
	return nil
}

func (f *fakeGames) AbortTournamentGame(_ context.Context, id uuid.UUID) error {
	f.aborted[id]++
	return nil
}

type fakeCancellations map[uuid.UUID]bool

func (f fakeCancellations) Cancel(_ context.Context, id uuid.UUID) error { f[id] = true; return nil }

func (f fakeCancellations) Cancelled(_ context.Context, id uuid.UUID) (bool, error) {
	return f[id], nil
}

func record(t *testing.T, topic string, message proto.Message) *kgo.Record {
	t.Helper()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	kind := string(message.ProtoReflect().Descriptor().FullName())
	return &kgo.Record{Topic: topic, Value: raw, Headers: []kgo.RecordHeader{{Key: outbox.TypeHeader, Value: []byte(kind)}}}
}

func pairing(id uuid.UUID, round, board int, players ...uuid.UUID) *eventsv1.TournamentPairing {
	out := &eventsv1.TournamentPairing{Board: uint32(board), SessionId: tournamentid.Session(id, round, board).String()}
	for _, player := range players {
		out.Players = append(out.Players, player.String())
	}
	return out
}

func consumer(games *fakeGames, cancelled fakeCancellations) (*tournament.Consumer, *prometheus.CounterVec) {
	counter := tournament.NewCounter(prometheus.NewRegistry())
	return tournament.NewConsumer(tournament.Config{Games: games, Cancellations: cancelled, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Commands: counter}), counter
}

func TestRoundStartedCreatesEachBoardOnceEvenWhenRedelivered(t *testing.T) {
	games, cancelled := newGames(), fakeCancellations{}
	c, counter := consumer(games, cancelled)
	id := uuid.New()
	a, b, x, y := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	event := record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{
		EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 1,
		Pairings: []*eventsv1.TournamentPairing{pairing(id, 1, 1, a, b), pairing(id, 1, 2, x, y)},
	})
	for range 3 {
		if err := c.Handle(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	if len(games.started) != 2 {
		t.Fatalf("started %d sessions, want 2", len(games.started))
	}
	first := games.started[tournamentid.Session(id, 1, 1)]
	if first.Players != [2]uuid.UUID{a, b} || first.Game != "tictactoe" || first.Tournament != id || first.Round != 1 || first.Board != 1 {
		t.Fatalf("board 1 = %+v", first)
	}
	if got := testutil.ToFloat64(counter.WithLabelValues(tournament.CommandRound, tournament.ResultApplied)); got != 3 {
		t.Fatalf("applied = %v", got)
	}
}

func TestForfeitEnsuresTheSessionAndCancelledTournamentsAreSkipped(t *testing.T) {
	games, cancelled := newGames(), fakeCancellations{}
	c, counter := consumer(games, cancelled)
	id, a, b := uuid.New(), uuid.New(), uuid.New()
	forfeit := record(t, tournament.ForfeitTopic, &eventsv1.TournamentForfeit{
		EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 2, Pairing: pairing(id, 2, 3, a, b),
	})
	if err := c.Handle(context.Background(), forfeit); err != nil {
		t.Fatal(err)
	}
	session := tournamentid.Session(id, 2, 3)
	if games.forfeited[session] != 1 || games.started[session].Players != [2]uuid.UUID{a, b} {
		t.Fatalf("forfeited %v, started %+v", games.forfeited, games.started)
	}
	other := uuid.New()
	cancel := record(t, tournament.CancelledTopic, &eventsv1.TournamentCancelled{
		EventId: uuid.NewString(), TournamentId: id.String(), Started: true, SessionIds: []string{session.String(), other.String()},
	})
	for range 2 {
		if err := c.Handle(context.Background(), cancel); err != nil {
			t.Fatal(err)
		}
	}
	if !cancelled[id] || games.aborted[session] != 2 || games.aborted[other] != 2 {
		t.Fatalf("cancelled %v, aborted %v", cancelled, games.aborted)
	}
	late := record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{
		EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 3, Pairings: []*eventsv1.TournamentPairing{pairing(id, 3, 1, a, b)},
	})
	if err := c.Handle(context.Background(), late); err != nil {
		t.Fatal(err)
	}
	if err := c.Handle(context.Background(), forfeit); err != nil {
		t.Fatal(err)
	}
	if _, ok := games.started[tournamentid.Session(id, 3, 1)]; ok || games.forfeited[session] != 1 {
		t.Fatal("a cancelled tournament created or forfeited a game")
	}
	if got := testutil.ToFloat64(counter.WithLabelValues(tournament.CommandRound, tournament.ResultSkipped)) +
		testutil.ToFloat64(counter.WithLabelValues(tournament.CommandForfeit, tournament.ResultSkipped)); got != 2 {
		t.Fatalf("skipped = %v", got)
	}
}

func TestMalformedCommandsAreSkippedAndFailuresRetried(t *testing.T) {
	games, cancelled := newGames(), fakeCancellations{}
	c, counter := consumer(games, cancelled)
	id, a, b := uuid.New(), uuid.New(), uuid.New()
	wrong := pairing(id, 1, 1, a, b)
	wrong.SessionId = uuid.NewString()
	for name, rec := range map[string]*kgo.Record{
		"foreign session id": record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairings: []*eventsv1.TournamentPairing{wrong}}),
		"one player":         record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairings: []*eventsv1.TournamentPairing{pairing(id, 1, 1, a)}}),
		"same player twice":  record(t, tournament.ForfeitTopic, &eventsv1.TournamentForfeit{TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairing: pairing(id, 1, 1, a, a)}),
		"no tournament":      record(t, tournament.CancelledTopic, &eventsv1.TournamentCancelled{}),
		"foreign event type": record(t, tournament.ForfeitTopic, &eventsv1.GameFinished{SessionId: id.String()}),
		"no game":            record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{TournamentId: id.String(), Round: 1}),
		"garbage":            {Topic: tournament.CancelledTopic, Value: []byte{0xff, 0x01}},
	} {
		if err := c.Handle(context.Background(), rec); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(games.started) != 0 || testutil.ToFloat64(counter.WithLabelValues(tournament.CommandUnknown, tournament.ResultInvalid)) != 7 {
		t.Fatalf("started %v", games.started)
	}
	games.fail = errors.New("owner moved")
	err := c.Handle(context.Background(), record(t, tournament.RoundStartedTopic, &eventsv1.TournamentRoundStarted{
		TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairings: []*eventsv1.TournamentPairing{pairing(id, 1, 1, a, b)},
	}))
	if err == nil || !errors.Is(err, games.fail) {
		t.Fatalf("a failing start = %v, want a retriable error", err)
	}
}

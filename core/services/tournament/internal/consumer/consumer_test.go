package consumer_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/tournament/internal/consumer"
	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

type recorder struct {
	results []orchestrator.Result
	fail    error
}

func (r *recorder) Record(_ context.Context, result orchestrator.Result) (string, error) {
	if r.fail != nil {
		return "", r.fail
	}
	r.results = append(r.results, result)
	return orchestrator.ResultApplied, nil
}

type ratings struct {
	game string
	got  map[uuid.UUID]int
}

func (r *ratings) SetRatings(_ context.Context, game string, _ time.Time, values map[uuid.UUID]int) error {
	r.game, r.got = game, values
	return nil
}

func record(t *testing.T, topic string, message proto.Message) *kgo.Record {
	t.Helper()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Topic: topic, Value: raw, Headers: []kgo.RecordHeader{{Key: outbox.TypeHeader, Value: []byte(message.ProtoReflect().Descriptor().FullName())}}}
}

var at = time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC)

func finished(tournament *string, plies *uint32) *eventsv1.GameFinished {
	return &eventsv1.GameFinished{EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: "tictactoe", Players: []string{uuid.NewString(), uuid.NewString()},
		Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 1, FinishedAt: timestamppb.New(at), Rated: true, TournamentId: tournament, Plies: plies}
}

func TestDecodeFinished(t *testing.T) {
	tournament := uuid.NewString()
	event := finished(&tournament, proto.Uint32(1))
	raw, _ := proto.Marshal(event)
	got, err := consumer.DecodeFinished(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tournament.String() != tournament || got.Session.String() != event.GetSessionId() || got.Winner != 1 || got.Plies != 1 || !got.FinishedAt.Equal(at) ||
		got.Players[0].String() != event.GetPlayers()[0] {
		t.Fatalf("decoded = %+v", got)
	}
	legacy := finished(&tournament, nil)
	legacy.Outcome, legacy.WinnerSeat = eventsv1.GameFinished_OUTCOME_DRAW, -1
	raw, _ = proto.Marshal(legacy)
	if got, err := consumer.DecodeFinished(raw); err != nil || got.Plies != 2 || got.Winner != saga.NoWinner {
		t.Fatalf("draw without plies = %+v %v", got, err)
	}
	raw, _ = proto.Marshal(finished(nil, nil))
	if _, err := consumer.DecodeFinished(raw); !errors.Is(err, consumer.ErrForeign) {
		t.Fatalf("casual game = %v", err)
	}
	bad := map[string]func(*eventsv1.GameFinished){
		"tournament": func(e *eventsv1.GameFinished) { e.TournamentId = proto.String("x") },
		"players":    func(e *eventsv1.GameFinished) { e.Players = e.Players[:1] },
		"seat":       func(e *eventsv1.GameFinished) { e.WinnerSeat = 2 },
		"outcome":    func(e *eventsv1.GameFinished) { e.Outcome = 0 },
		"time":       func(e *eventsv1.GameFinished) { e.FinishedAt = nil },
		"session":    func(e *eventsv1.GameFinished) { e.SessionId = "" },
	}
	for name, edit := range bad {
		event := finished(&tournament, nil)
		edit(event)
		raw, _ := proto.Marshal(event)
		if _, err := consumer.DecodeFinished(raw); !errors.Is(err, consumer.ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestHandleRoutesByEventType(t *testing.T) {
	rec, store := &recorder{}, &ratings{}
	c := consumer.New(consumer.Config{Recorder: rec, Ratings: store, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx := context.Background()
	tournament := uuid.NewString()
	user := uuid.New()
	for _, r := range []*kgo.Record{
		record(t, consumer.FinishedTopic, finished(&tournament, proto.Uint32(5))),
		record(t, consumer.FinishedTopic, finished(nil, nil)),
		{Topic: consumer.FinishedTopic, Value: []byte{0xff}},
		record(t, consumer.RatingTopic, &eventsv1.RatingChanged{Game: "tictactoe", ChangedAt: timestamppb.New(at),
			Changes: []*eventsv1.RatingChange{{UserId: user.String(), After: 1612}}}),
		record(t, consumer.RatingTopic, &eventsv1.RatingChanged{Game: "tictactoe"}),
		record(t, "other", &eventsv1.ChatMessageSent{}),
	} {
		if err := c.Handle(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if len(rec.results) != 1 || rec.results[0].Plies != 5 || store.game != "tictactoe" || store.got[user] != 1612 {
		t.Fatalf("results %+v, ratings %s %v", rec.results, store.game, store.got)
	}
	rec.fail = errors.New("postgres is gone")
	if err := c.Handle(ctx, record(t, consumer.FinishedTopic, finished(&tournament, nil))); !errors.Is(err, rec.fail) {
		t.Fatalf("a failing record = %v, want a retry", err)
	}
}

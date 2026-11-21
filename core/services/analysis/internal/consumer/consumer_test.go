package consumer_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/analysis/internal/consumer"
	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

type memory struct {
	processed map[uuid.UUID]bool
	reports   map[uuid.UUID]store.Status
	finished  []store.Outcome
	refuse    bool
	lose      bool
	fail      error
}

func newMemory() *memory {
	return &memory{processed: map[uuid.UUID]bool{}, reports: map[uuid.UUID]store.Status{}}
}

func (m *memory) Processed(_ context.Context, event uuid.UUID) (bool, error) {
	return m.processed[event], m.fail
}

func (m *memory) Start(_ context.Context, job store.Job) (bool, error) {
	if m.refuse {
		return false, nil
	}
	m.reports[job.Session] = store.Pending
	return true, nil
}

func (m *memory) Finish(_ context.Context, job store.Job, outcome store.Outcome) (bool, error) {
	if m.lose {
		return false, nil
	}
	m.processed[job.Event] = true
	m.reports[job.Session] = outcome.Status()
	m.finished = append(m.finished, outcome)
	return true, nil
}

type engine struct {
	calls     int
	err       error
	positions []review.Position
	block     bool
	moves     []string
}

func (e *engine) Analyse(ctx context.Context, moves []string) ([]review.Position, error) {
	e.calls++
	e.moves = moves
	if e.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if e.err != nil {
		return nil, e.err
	}
	if e.positions != nil {
		return e.positions, nil
	}
	out := make([]review.Position, len(moves)+1)
	for ply := range out {
		out[ply] = review.Position{Ply: ply, Class: review.ClassNone}
	}
	return out, nil
}

var scholar = []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}

func event(game string, moves []string) *eventsv1.GameFinished {
	return &eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: game, Players: []string{uuid.NewString(), uuid.NewString()},
		Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 0, FinishedAt: timestamppb.Now(), Moves: moves, Rated: true,
	}
}

func record(t *testing.T, kind string, message proto.Message) *kgo.Record {
	t.Helper()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Topic: consumer.Topic, Value: raw, Headers: []kgo.RecordHeader{{Key: "event_type", Value: []byte(kind)}}}
}

func setup(m *memory, e *engine) (*consumer.Consumer, *consumer.Metrics) {
	return setupWith(m, e, 0)
}

func setupWith(m *memory, e *engine, maxPlies int) (*consumer.Consumer, *consumer.Metrics) {
	metrics := consumer.NewMetrics(prometheus.NewRegistry())
	return consumer.New(consumer.Config{Store: m, Analyser: e, Depth: 12, Engine: "Stockfish 17.1", MaxPlies: maxPlies, Log: slog.New(slog.DiscardHandler), Metrics: metrics}), metrics
}

func outcomes(metrics *consumer.Metrics) map[string]float64 {
	out := map[string]float64{}
	for _, name := range []string{consumer.OutcomeReady, consumer.OutcomeFailed, consumer.OutcomeDuplicate, consumer.OutcomeSkipped, consumer.OutcomeInvalid} {
		if value := testutil.ToFloat64(metrics.Jobs.WithLabelValues(name)); value > 0 {
			out[name] = value
		}
	}
	return out
}

func observed(metrics *consumer.Metrics) uint64 {
	var sample dto.Metric
	if err := metrics.Duration.Write(&sample); err != nil {
		return 0
	}
	return sample.GetHistogram().GetSampleCount()
}

const finishedType = "axon.events.v1.GameFinished"

func TestAChessGameIsAnalysedOnce(t *testing.T) {
	m, e := newMemory(), &engine{}
	c, metrics := setup(m, e)
	finished := event("chess", scholar)
	rec := record(t, finishedType, finished)
	for range 3 {
		if err := c.Handle(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
	}
	session := uuid.MustParse(finished.SessionId)
	if e.calls != 1 || len(m.finished) != 1 || m.reports[session] != store.Ready || len(m.finished[0].Positions) != len(scholar)+1 {
		t.Fatalf("calls %d, finished %d, report %s", e.calls, len(m.finished), m.reports[session])
	}
	if got := outcomes(metrics); got[consumer.OutcomeReady] != 1 || got[consumer.OutcomeDuplicate] != 2 || len(got) != 2 {
		t.Fatalf("outcomes = %v", got)
	}
	if observed(metrics) != 1 {
		t.Fatalf("analysis time observed %d times", observed(metrics))
	}
}

func TestEventsThatAreNotChessAreSkipped(t *testing.T) {
	m, e := newMemory(), &engine{}
	c, metrics := setup(m, e)
	for _, rec := range []*kgo.Record{
		record(t, finishedType, event("tictactoe", nil)),
		record(t, finishedType, event("quoridor", []string{"e2"})),
		record(t, "axon.events.v1.RatingChanged", &eventsv1.RatingChanged{EventId: uuid.NewString()}),
	} {
		if err := c.Handle(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
	}
	if e.calls != 0 || len(m.reports) != 0 || outcomes(metrics)[consumer.OutcomeSkipped] != 3 {
		t.Fatalf("calls %d, reports %d, outcomes %v", e.calls, len(m.reports), outcomes(metrics))
	}
}

func TestInvalidEventsAreSkipped(t *testing.T) {
	m, e := newMemory(), &engine{}
	c, metrics := setup(m, e)
	broken := event("chess", scholar)
	broken.SessionId = "not-a-uuid"
	late := event("chess", scholar)
	late.FinishedAt = nil
	for _, rec := range []*kgo.Record{
		record(t, finishedType, broken),
		record(t, finishedType, late),
		{Topic: consumer.Topic, Value: []byte{0xff, 0x01}},
	} {
		if err := c.Handle(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
	}
	if e.calls != 0 || outcomes(metrics)[consumer.OutcomeInvalid] != 3 {
		t.Fatalf("calls %d, outcomes %v", e.calls, outcomes(metrics))
	}
}

func TestAnEngineFailureIsRecorded(t *testing.T) {
	m, e := newMemory(), &engine{err: errors.New("uci: position search timed out")}
	c, metrics := setup(m, e)
	finished := event("chess", scholar)
	if err := c.Handle(t.Context(), record(t, finishedType, finished)); err != nil {
		t.Fatal(err)
	}
	if m.reports[uuid.MustParse(finished.SessionId)] != store.Failed || m.finished[0].Err != "uci: position search timed out" || m.finished[0].Positions != nil {
		t.Fatalf("finished = %+v", m.finished)
	}
	if got := outcomes(metrics); got[consumer.OutcomeFailed] != 1 || len(got) != 1 || observed(metrics) != 0 {
		t.Fatalf("outcomes = %v, observed %d", got, observed(metrics))
	}
}

func TestAGameWithoutMovesFails(t *testing.T) {
	m, e := newMemory(), &engine{}
	c, _ := setup(m, e)
	finished := event("chess", nil)
	if err := c.Handle(t.Context(), record(t, finishedType, finished)); err != nil {
		t.Fatal(err)
	}
	if e.calls != 0 || m.reports[uuid.MustParse(finished.SessionId)] != store.Failed || m.finished[0].Err != consumer.ErrNoMoves.Error() {
		t.Fatalf("calls %d, finished %+v", e.calls, m.finished)
	}
}

func TestAnInterruptedAnalysisIsLeftForRedelivery(t *testing.T) {
	m, e := newMemory(), &engine{block: true}
	c, metrics := setup(m, e)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	finished := event("chess", scholar)
	if err := c.Handle(ctx, record(t, finishedType, finished)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if len(m.finished) != 0 || m.reports[uuid.MustParse(finished.SessionId)] != store.Pending || len(outcomes(metrics)) != 0 {
		t.Fatalf("finished %+v, outcomes %v", m.finished, outcomes(metrics))
	}
}

func TestRacesWithAnotherDeliveryAreDuplicates(t *testing.T) {
	for name, m := range map[string]*memory{
		"report already final":      {processed: map[uuid.UUID]bool{}, reports: map[uuid.UUID]store.Status{}, refuse: true},
		"event processed meanwhile": {processed: map[uuid.UUID]bool{}, reports: map[uuid.UUID]store.Status{}, lose: true},
	} {
		t.Run(name, func(t *testing.T) {
			c, metrics := setup(m, &engine{})
			if err := c.Handle(t.Context(), record(t, finishedType, event("chess", scholar))); err != nil {
				t.Fatal(err)
			}
			if got := outcomes(metrics); got[consumer.OutcomeDuplicate] != 1 || len(got) != 1 {
				t.Fatalf("outcomes = %v", got)
			}
		})
	}
}

func TestAStoreErrorKeepsTheRecordUncommitted(t *testing.T) {
	m := newMemory()
	m.fail = errors.New("postgres is down")
	c, _ := setup(m, &engine{})
	if err := c.Handle(t.Context(), record(t, finishedType, event("chess", scholar))); err == nil {
		t.Fatal("a store error was swallowed")
	}
}

func TestDecodeKeepsTheMoves(t *testing.T) {
	finished := event("chess", scholar)
	raw, _ := proto.Marshal(finished)
	game, err := consumer.Decode("", raw)
	if err != nil {
		t.Fatal(err)
	}
	if game.EventID.String() != finished.EventId || game.Session.String() != finished.SessionId || len(game.Moves) != 7 || game.FinishedAt.IsZero() {
		t.Fatalf("game = %+v", game)
	}
}

func TestUnratedGamesAreSkipped(t *testing.T) {
	m, e := newMemory(), &engine{}
	c, metrics := setup(m, e)
	casual := event("chess", scholar)
	casual.Rated = false
	if err := c.Handle(t.Context(), record(t, finishedType, casual)); err != nil {
		t.Fatal(err)
	}
	if e.calls != 0 || len(m.reports) != 0 || outcomes(metrics)[consumer.OutcomeSkipped] != 1 {
		t.Fatalf("calls %d, reports %d, outcomes %v", e.calls, len(m.reports), outcomes(metrics))
	}
}

func TestTheEngineSeesAtMostTheMaximumPlies(t *testing.T) {
	long := make([]string, 0, 400)
	for len(long) < 400 {
		long = append(long, "g1f3", "g8f6", "f3g1", "f6g8")
	}
	for name, tt := range map[string]struct {
		limit, want int
	}{
		"default": {limit: 0, want: consumer.DefaultMaxPlies},
		"custom":  {limit: 6, want: 6},
		"short":   {limit: 500, want: 400},
	} {
		t.Run(name, func(t *testing.T) {
			m, e := newMemory(), &engine{}
			c, _ := setupWith(m, e, tt.limit)
			if err := c.Handle(t.Context(), record(t, finishedType, event("chess", long))); err != nil {
				t.Fatal(err)
			}
			if len(e.moves) != tt.want || len(m.finished) != 1 || len(m.finished[0].Positions) != tt.want+1 {
				t.Fatalf("engine saw %d moves, finished %d reports, want %d moves", len(e.moves), len(m.finished), tt.want)
			}
		})
	}
}

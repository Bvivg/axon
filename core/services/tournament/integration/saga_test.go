//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/tournament/internal/consumer"
	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

type harness struct {
	t        *testing.T
	store    *store.Postgres
	saga     *orchestrator.Orchestrator
	metrics  *orchestrator.Metrics
	topic    string
	group    string
	producer *kgo.Client
}

func newHarness(t *testing.T, noShow time.Duration) *harness {
	t.Helper()
	suffix := uuid.NewString()[:8]
	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(producer.Close)
	tournaments := store.New(db)
	metrics := orchestrator.NewMetrics(prometheus.NewRegistry())
	return &harness{
		t: t, store: tournaments, metrics: metrics, topic: "game.finished.it-" + suffix, group: "tournament-it-" + suffix, producer: producer,
		saga: orchestrator.New(orchestrator.Config{Store: tournaments, NoShow: noShow, Metrics: metrics, Log: quiet}),
	}
}

type instance struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (i *instance) stop() {
	i.cancel()
	<-i.done
}

func (h *harness) start(sweep time.Duration) *instance {
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{cancel: cancel, done: make(chan struct{})}
	events := consumer.New(consumer.Config{Brokers: brokers, FinishedTopic: h.topic, RatingTopic: h.topic + "-ratings", Group: h.group,
		Recorder: h.saga, Ratings: h.store, Log: quiet})
	go func() {
		defer close(in.done)
		go h.saga.Run(ctx, sweep)
		_ = events.Run(ctx)
	}()
	h.t.Cleanup(in.stop)
	return in
}

func (h *harness) open(format saga.Format, capacity, rounds, players int) (saga.Tournament, []uuid.UUID) {
	h.t.Helper()
	ctx := context.Background()
	creator := uuid.New()
	t, err := h.saga.Create(ctx, creator, uuid.New(), saga.Spec{Name: "Integration cup", Game: "tictactoe", Format: format, Capacity: capacity, Rounds: rounds})
	if err != nil {
		h.t.Fatal(err)
	}
	ids := make([]uuid.UUID, players)
	for i := range ids {
		ids[i] = uuid.New()
		if t, err = h.saga.Register(ctx, t.ID, ids[i]); err != nil {
			h.t.Fatal(err)
		}
	}
	return t, ids
}

func (h *harness) finish(t saga.Tournament, m saga.Match, winner, plies int, event uuid.UUID) {
	h.t.Helper()
	id := t.ID.String()
	payload := &eventsv1.GameFinished{EventId: event.String(), SessionId: m.Session.String(), Game: "tictactoe",
		Players: []string{m.Seats[0].String(), m.Seats[1].String()}, Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: int32(winner),
		FinishedAt: timestamppb.Now(), Rated: true, TournamentId: &id, Plies: proto.Uint32(uint32(plies))}
	raw, err := proto.Marshal(payload)
	if err != nil {
		h.t.Fatal(err)
	}
	record := &kgo.Record{Topic: h.topic, Key: []byte("tictactoe"), Value: raw, Headers: []kgo.RecordHeader{
		{Key: outbox.TypeHeader, Value: []byte(payload.ProtoReflect().Descriptor().FullName())}, {Key: outbox.IDHeader, Value: []byte(event.String())},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.producer.ProduceSync(ctx, record).FirstErr(); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *harness) published(t saga.Tournament, topic string) int {
	return h.count(`SELECT count(*) FROM tournament.outbox WHERE aggregateid = $1 AND aggregatetype = $2`, t.ID.String(), topic)
}

func (h *harness) eventually(what string, check func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			h.t.Fatal("timed out waiting for", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (h *harness) load(id uuid.UUID) saga.Tournament {
	h.t.Helper()
	t, err := h.store.Get(context.Background(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	return t
}

func (h *harness) decided(id uuid.UUID, round int) int {
	n := 0
	for _, m := range h.load(id).RoundMatches(round) {
		if m.Decided() {
			n++
		}
	}
	return n
}

func TestBracketSagaSurvivesRedeliveryAndAnOrchestratorRestart(t *testing.T) {
	h := newHarness(t, time.Hour)
	ctx := context.Background()
	t0 := time.Now()
	tour, players := h.open(saga.SingleElimination, 8, 0, 8)
	tour, err := h.saga.Start(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tour.Status != saga.Running || tour.Round != 1 || h.published(tour, orchestrator.RoundStartedTopic) != 1 ||
		h.count(`SELECT count(*) FROM tournament.deadlines WHERE tournament_id = $1`, tour.ID) != 4 {
		t.Fatalf("after the start: %s round %d", tour.Status, tour.Round)
	}
	if again, err := h.saga.Start(ctx, tour.ID); err != nil || again.Round != 1 || h.published(tour, orchestrator.RoundStartedTopic) != 1 {
		t.Fatalf("a second start = %v, round %d", err, again.Round)
	}
	first := h.start(100 * time.Millisecond)
	round := tour.RoundMatches(1)
	ids := map[uuid.UUID]uuid.UUID{}
	sent, unique := 0, 0
	result := func(m saga.Match, copies int) {
		if _, ok := ids[m.Session]; !ok {
			ids[m.Session] = uuid.New()
			unique++
		}
		for range copies {
			h.finish(tour, m, 0, 5, ids[m.Session])
			sent++
		}
	}
	result(round[0], 2)
	result(round[1], 2)
	h.finish(tour, round[0], 1, 5, uuid.New())
	sent, unique = sent+1, unique+1
	h.eventually("two boards of round 1", func() bool { return h.decided(tour.ID, 1) == 2 })
	first.stop()

	result(round[2], 2)
	result(round[3], 3)
	time.Sleep(2 * time.Second)
	if n := h.decided(tour.ID, 1); n != 2 {
		t.Fatalf("results were applied while the orchestrator was down: %d", n)
	}
	h.start(100 * time.Millisecond)
	h.eventually("round 2 after the restart", func() bool { return h.published(tour, orchestrator.RoundStartedTopic) == 2 })
	for _, m := range h.load(tour.ID).RoundMatches(2) {
		result(m, 2)
	}
	h.eventually("the final", func() bool { return h.published(tour, orchestrator.RoundStartedTopic) == 3 })
	final := h.load(tour.ID).RoundMatches(3)
	result(final[0], 3)
	h.eventually("the tournament to finish", func() bool { return h.load(tour.ID).Status == saga.Finished })
	elapsed := time.Since(t0)
	result(round[1], 1)
	time.Sleep(time.Second)

	done := h.load(tour.ID)
	seeds := map[uuid.UUID]int{}
	for _, p := range done.Players {
		seeds[p.User] = p.Seed
	}
	if seeds[done.Winner] != 1 || seeds[players[0]] != 1 || len(done.Matches) != 7 || done.Round != 3 {
		t.Fatalf("winner seed %d, %d matches over %d rounds", seeds[done.Winner], len(done.Matches), done.Round)
	}
	board1, _ := done.Match(round[0].Session)
	if board1.Winner != round[0].Seats[0] {
		t.Fatalf("a replayed result with a new event id overrode board 1: %+v", board1)
	}
	if n := h.count(`SELECT count(*) FROM tournament.processed_events WHERE session_id IN (SELECT session_id FROM tournament.matches WHERE tournament_id = $1)`, tour.ID); n != unique {
		t.Fatalf("processed events = %d, want %d", n, unique)
	}
	if h.published(tour, orchestrator.RoundStartedTopic) != 3 || h.published(tour, orchestrator.FinishedTopic) != 1 || h.published(tour, orchestrator.ForfeitTopic) != 0 {
		t.Fatal("unexpected outbox events")
	}
	applied := testutil.ToFloat64(h.metrics.Events.WithLabelValues(orchestrator.ResultApplied))
	duplicates := testutil.ToFloat64(h.metrics.Events.WithLabelValues(orchestrator.ResultDuplicate))
	if applied != 7 || int(applied+duplicates) < sent {
		t.Fatalf("applied %v, duplicates %v, sent %d", applied, duplicates, sent)
	}
	var payload []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM tournament.outbox WHERE aggregateid = $1 AND aggregatetype = $2`, tour.ID.String(), orchestrator.FinishedTopic).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	announced := &eventsv1.TournamentFinished{}
	if err := proto.Unmarshal(payload, announced); err != nil || announced.GetWinnerId() != done.Winner.String() || len(announced.GetStandings()) != 8 {
		t.Fatalf("tournament.finished = %v, %v", announced, err)
	}
	t.Logf("8-player bracket finished in %s: %d game.finished sent, %d redelivered by Kafka after the restart, %d applied, %d duplicates, 3 round_started, 1 finished",
		elapsed.Round(time.Millisecond), sent, int(applied+duplicates)-sent, int(applied), int(duplicates))
}

func TestStalledStepsResumeAfterACrash(t *testing.T) {
	h := newHarness(t, time.Hour)
	ctx := context.Background()
	seeding, _ := h.open(saga.SingleElimination, 4, 0, 4)
	if _, err := h.store.Update(ctx, seeding.ID, func(_ context.Context, _ *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		next, _, err := t.Start(time.Now())
		return next, err
	}); err != nil {
		t.Fatal(err)
	}
	between, _ := h.open(saga.Swiss, 4, 2, 4)
	between, err := h.saga.Start(ctx, between.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range between.RoundMatches(1) {
		if _, err := h.store.Update(ctx, between.ID, func(_ context.Context, _ *store.Tx, t saga.Tournament) (saga.Tournament, error) {
			next, _, err := t.Record(m.Session, saga.Outcome{Players: m.Seats, Winner: 0, Plies: 5, At: time.Now()})
			return next, err
		}); err != nil {
			t.Fatal(err)
		}
	}
	if h.load(seeding.ID).Status != saga.Seeding || !h.load(between.ID).RoundComplete() || h.published(between, orchestrator.RoundStartedTopic) != 1 {
		t.Fatal("the crash was not simulated")
	}
	h.start(100 * time.Millisecond)
	h.eventually("the seeding tournament to start round 1", func() bool { return h.published(seeding, orchestrator.RoundStartedTopic) == 1 })
	h.eventually("the swiss tournament to start round 2", func() bool { return h.published(between, orchestrator.RoundStartedTopic) == 2 })
	resumed := h.load(between.ID)
	if resumed.Round != 2 || len(resumed.RoundMatches(2)) != 2 {
		t.Fatalf("resumed round %d", resumed.Round)
	}
	met := map[[2]uuid.UUID]bool{}
	for _, m := range resumed.Matches {
		if met[m.Seats] || met[[2]uuid.UUID{m.Seats[1], m.Seats[0]}] {
			t.Fatalf("round 2 repeats %v", m.Seats)
		}
		met[m.Seats] = true
	}
}

func TestNoShowDeadlineForfeitsOnceAcrossInstances(t *testing.T) {
	const noShow = 2 * time.Second
	h := newHarness(t, noShow)
	ctx := context.Background()
	tour, _ := h.open(saga.SingleElimination, 4, 0, 4)
	tour, err := h.saga.Start(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.start(200 * time.Millisecond)
	secondMetrics := orchestrator.NewMetrics(prometheus.NewRegistry())
	second := orchestrator.New(orchestrator.Config{Store: h.store, NoShow: noShow, Metrics: secondMetrics, Log: quiet})
	sweeper, stop := context.WithCancel(context.Background())
	go second.Run(sweeper, 150*time.Millisecond)
	t.Cleanup(stop)
	round := tour.RoundMatches(1)
	h.eventually("both forfeits", func() bool { return h.published(tour, orchestrator.ForfeitTopic) == 2 })
	time.Sleep(time.Second)
	if n := h.published(tour, orchestrator.ForfeitTopic); n != 2 {
		t.Fatalf("%d forfeit commands for 2 games", n)
	}
	var lag float64
	if err := db.QueryRow(ctx, `SELECT EXTRACT(EPOCH FROM (SELECT max(created_at) FROM tournament.outbox WHERE aggregateid = $1 AND aggregatetype = $2) -
		(SELECT max(due_at) FROM tournament.deadlines WHERE tournament_id = $3))::float8`, tour.ID.String(), orchestrator.ForfeitTopic, tour.ID).Scan(&lag); err != nil {
		t.Fatal(err)
	}
	if h.count(`SELECT count(*) FROM tournament.deadlines WHERE tournament_id = $1 AND forfeited`, tour.ID) != 2 {
		t.Fatal("deadlines were not marked as forfeited")
	}
	h.finish(tour, round[0], 1, 0, uuid.New())
	h.finish(tour, round[1], 0, 1, uuid.New())
	h.eventually("the final", func() bool { return h.published(tour, orchestrator.RoundStartedTopic) == 2 })
	decided := h.load(tour.ID)
	for _, m := range decided.RoundMatches(1) {
		if !m.Forfeit {
			t.Fatalf("board %d is not a forfeit: %+v", m.Board, m)
		}
	}
	if forfeits := testutil.ToFloat64(h.metrics.Forfeits) + testutil.ToFloat64(secondMetrics.Forfeits); forfeits != 2 {
		t.Fatalf("forfeit counters = %v", forfeits)
	}
	t.Logf("no-show forfeit commands at most %.3fs after the %s deadline", lag, noShow)
}

func TestCancellationCompensates(t *testing.T) {
	h := newHarness(t, 2*time.Second)
	ctx := context.Background()
	early, players := h.open(saga.SingleElimination, 4, 0, 3)
	if len(players) != 3 {
		t.Fatal(len(players))
	}
	cancelled, err := h.saga.Cancel(ctx, early.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != saga.Cancelled || len(cancelled.Registered()) != 0 ||
		h.count(`SELECT count(*) FROM tournament.registrations WHERE tournament_id = $1 AND returned`, early.ID) != len(players) {
		t.Fatalf("early cancel: %s, %d still registered", cancelled.Status, len(cancelled.Registered()))
	}
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT payload FROM tournament.outbox WHERE aggregateid = $1 AND aggregatetype = $2`, early.ID.String(), orchestrator.CancelledTopic).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	event := &eventsv1.TournamentCancelled{}
	if err := proto.Unmarshal(raw, event); err != nil || event.GetStarted() || len(event.GetReturnedPlayers()) != 3 {
		t.Fatalf("early cancel event = %v %v", event, err)
	}
	if _, err := h.saga.Start(ctx, early.ID); err == nil {
		t.Fatal("a cancelled tournament started")
	}

	live, _ := h.open(saga.SingleElimination, 4, 0, 4)
	live, err = h.saga.Start(ctx, live.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.start(100 * time.Millisecond)
	round := live.RoundMatches(1)
	h.finish(live, round[0], 0, 5, uuid.New())
	h.eventually("board 1", func() bool { return h.decided(live.ID, 1) == 1 })
	if _, err := h.saga.Cancel(ctx, live.ID, false); err == nil {
		t.Fatal("a non-admin cancelled a running tournament")
	}
	stopped, err := h.saga.Cancel(ctx, live.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	aborted, _ := stopped.Match(round[1].Session)
	if stopped.Status != saga.Cancelled || aborted.Result != saga.Aborted {
		t.Fatalf("admin cancel: %s, board 2 %s", stopped.Status, aborted.Result)
	}
	if err := db.QueryRow(ctx, `SELECT payload FROM tournament.outbox WHERE aggregateid = $1 AND aggregatetype = $2`, live.ID.String(), orchestrator.CancelledTopic).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := proto.Unmarshal(raw, event); err != nil || !event.GetStarted() || len(event.GetSessionIds()) != 1 || event.GetSessionIds()[0] != round[1].Session.String() {
		t.Fatalf("running cancel event = %v %v", event, err)
	}
	h.finish(live, round[1], 0, 5, uuid.New())
	h.eventually("both deadlines to pass", func() bool {
		return h.count(`SELECT count(*) FROM tournament.deadlines WHERE tournament_id = $1 AND fired_at IS NOT NULL`, live.ID) == 2
	})
	time.Sleep(500 * time.Millisecond)
	after := h.load(live.ID)
	if late, _ := after.Match(round[1].Session); late.Result != saga.Aborted || h.published(live, orchestrator.ForfeitTopic) != 0 {
		t.Fatalf("after the cancellation: board 2 %s, %d forfeits", late.Result, h.published(live, orchestrator.ForfeitTopic))
	}
	if skipped := testutil.ToFloat64(h.metrics.Events.WithLabelValues(orchestrator.ResultSkipped)); skipped < 1 {
		t.Fatalf("the late result was not skipped: %v", skipped)
	}
}

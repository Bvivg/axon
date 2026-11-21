//go:build integration

package integration

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/analysis/internal/analyser"
	"github.com/bvivg/axon/core/services/analysis/internal/consumer"
	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	"github.com/bvivg/axon/core/services/analysis/internal/uci"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

var scholar = []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}

type dialer struct {
	mu    sync.Mutex
	conns []net.Conn
}

func (d *dialer) start(ctx context.Context) (*uci.Engine, error) {
	var dial net.Dialer
	conn, err := dial.DialContext(ctx, "tcp", stockfish)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.conns = append(d.conns, conn)
	d.mu.Unlock()
	return uci.Connect(ctx, conn, uci.Options{Threads: 1, HashMB: 16})
}

func engines(t *testing.T, size, depth int, timeout time.Duration) (*uci.Pool, *dialer, *atomic.Int32) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	d := &dialer{}
	var restarts atomic.Int32
	pool, err := uci.NewPool(ctx, uci.PoolConfig{Size: size, Depth: depth, Timeout: timeout, Start: d.start, Backoff: 100 * time.Millisecond,
		OnRestart: func() { restarts.Add(1) }})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		pool.Close()
	})
	return pool, d, &restarts
}

func eventually(t *testing.T, what string, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestScholarsMateIsAnalysedOnceThroughKafka(t *testing.T) {
	ctx := t.Context()
	engine, _, _ := engines(t, 2, 12, 10*time.Second)
	if engine.Name() == "" {
		t.Fatal("stockfish did not introduce itself")
	}
	topic, group := "game.analysis_requested.it-"+uuid.NewString()[:8], "analysis-it-"+uuid.NewString()[:8]
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	admin := kadm.NewClient(client)
	if _, err := admin.CreateTopic(ctx, 1, 1, nil, topic); err != nil {
		t.Fatal(err)
	}
	metrics := consumer.NewMetrics(prometheus.NewRegistry())
	reports := store.New(db)
	worker := consumer.New(consumer.Config{Brokers: brokers, Topic: topic, Group: group, Store: reports, Analyser: analyser.New(engine),
		Depth: engine.Depth(), Engine: engine.Name(), Log: slog.New(slog.DiscardHandler), Metrics: metrics})
	running, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(running)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	finished := &eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: "chess", Players: []string{uuid.NewString(), uuid.NewString()},
		Outcome: eventsv1.GameFinished_OUTCOME_WIN, WinnerSeat: 0, FinishedAt: timestamppb.Now(), Moves: scholar, Rated: true,
	}
	raw, err := proto.Marshal(finished)
	if err != nil {
		t.Fatal(err)
	}
	deliver := func() {
		t.Helper()
		record := &kgo.Record{Topic: topic, Key: []byte(finished.SessionId), Value: raw, Headers: []kgo.RecordHeader{
			{Key: "event_id", Value: []byte(finished.EventId)}, {Key: "event_type", Value: []byte("axon.events.v1.GameFinished")},
		}}
		if err := client.ProduceSync(ctx, record).FirstErr(); err != nil {
			t.Fatal(err)
		}
	}
	committed := func(want int64) func() bool {
		return func() bool {
			offsets, err := admin.FetchOffsets(ctx, group)
			if err != nil {
				return false
			}
			at, ok := offsets.Lookup(topic, 0)
			return ok && at.At == want
		}
	}
	began := time.Now()
	deliver()
	deliver()
	session := uuid.MustParse(finished.SessionId)
	var report store.Report
	eventually(t, "the analysis", time.Minute, func() bool {
		report, err = reports.Get(ctx, session)
		return err == nil && report.Status != store.Pending
	})
	t.Logf("%s analysed %d positions at depth %d in %s (engine time %s)", report.Engine, len(report.Positions), report.Depth, time.Since(began).Round(time.Millisecond), report.Duration)
	if report.Status != store.Ready || len(report.Positions) != len(scholar)+1 {
		t.Fatalf("report = %+v", report)
	}
	blunder, mate := report.Positions[6], report.Positions[7]
	if blunder.Played != "g8f6" || blunder.Class != review.Blunder || blunder.Mate == nil || *blunder.Mate != 1 || blunder.Centipawns != review.MateScore {
		t.Fatalf("ply 6 = %+v", blunder)
	}
	if mate.Played != "h5f7" || mate.Class != review.Best || mate.Mate == nil || *mate.Mate != 0 || mate.BestMove != "" || mate.Centipawns != review.MateScore {
		t.Fatalf("ply 7 = %+v", mate)
	}
	if report.Positions[5].BestMove == "g8f6" || report.Positions[0].Played != "" {
		t.Fatalf("ply 5 = %+v, start = %+v", report.Positions[5], report.Positions[0])
	}
	eventually(t, "both deliveries committed", 30*time.Second, committed(2))
	deliver()
	eventually(t, "the third delivery committed", 30*time.Second, committed(3))
	for query, want := range map[string]int{
		`SELECT count(*) FROM analysis.reports WHERE session_id = $1`:          1,
		`SELECT count(*) FROM analysis.positions WHERE session_id = $1`:        len(scholar) + 1,
		`SELECT count(*) FROM analysis.processed_events WHERE session_id = $1`: 1,
	} {
		if n := rows(t, ctx, query, session); n != want {
			t.Fatalf("%s = %d, want %d", query, n, want)
		}
	}
	if ready, duplicates := testutil.ToFloat64(metrics.Jobs.WithLabelValues(consumer.OutcomeReady)), testutil.ToFloat64(metrics.Jobs.WithLabelValues(consumer.OutcomeDuplicate)); ready != 1 || duplicates != 2 {
		t.Fatalf("ready %v, duplicates %v", ready, duplicates)
	}
}

func TestAStockfishThatDiesIsReplaced(t *testing.T) {
	pool, d, restarts := engines(t, 1, 12, 10*time.Second)
	d.mu.Lock()
	_ = d.conns[0].Close()
	d.mu.Unlock()
	result, err := pool.Evaluate(t.Context(), scholar[:6])
	if err != nil {
		t.Fatal(err)
	}
	if !result.Score.Mate || result.Score.Value != 1 || result.BestMove != "h5f7" {
		t.Fatalf("result = %+v", result)
	}
	eventually(t, "the restart counted", 5*time.Second, func() bool { return restarts.Load() == 1 })
	if pool.Alive() != 1 || len(d.conns) != 2 {
		t.Fatalf("alive %d, dialled %d", pool.Alive(), len(d.conns))
	}
}

func TestASearchThatRunsTooLongIsStopped(t *testing.T) {
	pool, _, restarts := engines(t, 1, 40, 300*time.Millisecond)
	started := time.Now()
	if _, err := pool.Evaluate(t.Context(), nil); !errors.Is(err, uci.ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("the stop took %s", elapsed)
	}
	if pool.Alive() != 1 || restarts.Load() != 0 {
		t.Fatalf("an engine that answered stop was replaced: alive %d, restarts %d", pool.Alive(), restarts.Load())
	}
}

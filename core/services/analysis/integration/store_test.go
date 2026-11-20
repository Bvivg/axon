//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
)

func newJob(moves int) store.Job {
	return store.Job{Event: uuid.New(), Session: uuid.New(), Moves: moves, Depth: 12, Engine: "Stockfish 17.1", FinishedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func reviewed() []review.Position {
	return []review.Position{
		{Ply: 0, Centipawns: 38, BestMove: "g7g6", Class: review.ClassNone},
		{Ply: 1, Centipawns: 10000, Mate: new(1), BestMove: "h5f7", Played: "g8f6", Loss: 10038, Class: review.Blunder},
		{Ply: 2, Centipawns: 10000, Mate: new(0), Played: "h5f7", Class: review.Best},
	}
}

func rows(t *testing.T, ctx context.Context, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAReportGoesFromPendingToReadyOnce(t *testing.T) {
	ctx := t.Context()
	reports := store.New(db)
	job := newJob(2)
	if started, err := reports.Start(ctx, job); err != nil || !started {
		t.Fatal("start", started, err)
	}
	pending, err := reports.Get(ctx, job.Session)
	if err != nil || pending.Status != store.Pending || pending.CompletedAt != nil || len(pending.Positions) != 0 || pending.Engine != "Stockfish 17.1" {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
	if started, err := reports.Start(ctx, job); err != nil || !started {
		t.Fatal("a pending report could not be restarted after a crash", started, err)
	}
	if done, err := reports.Finish(ctx, job, store.Outcome{Positions: reviewed(), Duration: 1500 * time.Millisecond}); err != nil || !done {
		t.Fatal("finish", done, err)
	}
	ready, err := reports.Get(ctx, job.Session)
	if err != nil || ready.Status != store.Ready || ready.CompletedAt == nil || ready.Duration != 1500*time.Millisecond || ready.Err != "" || ready.Depth != 12 {
		t.Fatalf("ready = %+v, %v", ready, err)
	}
	want := reviewed()
	for i, p := range ready.Positions {
		w := want[i]
		if p.Ply != w.Ply || p.Centipawns != w.Centipawns || p.BestMove != w.BestMove || p.Played != w.Played || p.Loss != w.Loss || p.Class != w.Class ||
			(p.Mate == nil) != (w.Mate == nil) || (p.Mate != nil && *p.Mate != *w.Mate) {
			t.Fatalf("position %d = %+v, want %+v", i, p, w)
		}
	}
	if done, err := reports.Finish(ctx, job, store.Outcome{Positions: reviewed()}); err != nil || done {
		t.Fatal("a redelivered event finished again", done, err)
	}
	if started, err := reports.Start(ctx, job); err != nil || started {
		t.Fatal("a ready report went back to pending", started, err)
	}
	other := job
	other.Event = uuid.New()
	if started, err := reports.Start(ctx, other); err != nil || started {
		t.Fatal("a second event for an analysed game restarted it", started, err)
	}
	if seen, err := reports.Processed(ctx, job.Event); err != nil || !seen {
		t.Fatal("processed", seen, err)
	}
	if n := rows(t, ctx, `SELECT count(*) FROM analysis.positions WHERE session_id = $1`, job.Session); n != 3 {
		t.Fatalf("%d position rows", n)
	}
	if n := rows(t, ctx, `SELECT count(*) FROM analysis.processed_events WHERE session_id = $1`, job.Session); n != 1 {
		t.Fatalf("%d processed rows", n)
	}
}

func TestAFailedReportKeepsTheError(t *testing.T) {
	ctx := t.Context()
	reports := store.New(db)
	job := newJob(7)
	if _, err := reports.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	if done, err := reports.Finish(ctx, job, store.Outcome{Err: "analyser: ply 3: uci: engine exited", Duration: time.Second}); err != nil || !done {
		t.Fatal(done, err)
	}
	failed, err := reports.Get(ctx, job.Session)
	if err != nil || failed.Status != store.Failed || failed.Err != "analyser: ply 3: uci: engine exited" || len(failed.Positions) != 0 || failed.CompletedAt == nil {
		t.Fatalf("failed = %+v, %v", failed, err)
	}
}

func TestConcurrentDeliveriesFinishOnce(t *testing.T) {
	ctx := t.Context()
	reports := store.New(db)
	job := newJob(2)
	if _, err := reports.Start(ctx, job); err != nil {
		t.Fatal(err)
	}
	var finished atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			done, err := reports.Finish(ctx, job, store.Outcome{Positions: reviewed(), Duration: time.Second})
			if err != nil {
				t.Error(err)
			}
			if done {
				finished.Add(1)
			}
		})
	}
	wg.Wait()
	if finished.Load() != 1 {
		t.Fatalf("%d deliveries finished the report", finished.Load())
	}
	if n := rows(t, ctx, `SELECT count(*) FROM analysis.positions WHERE session_id = $1`, job.Session); n != 3 {
		t.Fatalf("%d position rows", n)
	}
}

func TestAnUnknownGameHasNoReport(t *testing.T) {
	if _, err := store.New(db).Get(t.Context(), uuid.New()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

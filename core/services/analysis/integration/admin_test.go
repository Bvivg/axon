//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/analysis/internal/store"
)

func TestReportsAreListedNewestFirstAndByStatus(t *testing.T) {
	ctx := t.Context()
	reports := store.New(db)
	ready, failed, pending := newJob(2), newJob(5), newJob(9)
	for _, job := range []store.Job{ready, failed, pending} {
		if started, err := reports.Start(ctx, job); err != nil || !started {
			t.Fatal("start", job.Session, started, err)
		}
	}
	if done, err := reports.Finish(ctx, ready, store.Outcome{Positions: reviewed(), Duration: 800 * time.Millisecond}); err != nil || !done {
		t.Fatal("finish ready", done, err)
	}
	if done, err := reports.Finish(ctx, failed, store.Outcome{Duration: 30 * time.Millisecond, Err: "uci: engine exited"}); err != nil || !done {
		t.Fatal("finish failed", done, err)
	}

	collect := func(status store.Status) []store.Report {
		t.Helper()
		var all []store.Report
		cursor := ""
		for range 1000 {
			page, next, err := reports.List(ctx, store.ReportFilter{Status: status, Cursor: cursor, Limit: 2})
			if err != nil {
				t.Fatalf("List(%q): %v", status, err)
			}
			if len(page) > 2 {
				t.Fatalf("a page of %d reports, want at most 2", len(page))
			}
			all = append(all, page...)
			if next == "" {
				return all
			}
			cursor = next
		}
		t.Fatal("paging never ended")
		return nil
	}

	position := map[uuid.UUID]int{}
	everything := collect("")
	for i, r := range everything {
		if _, seen := position[r.Session]; seen {
			t.Fatalf("session %s listed twice", r.Session)
		}
		position[r.Session] = i
		if i > 0 && everything[i-1].StartedAt.Before(r.StartedAt) {
			t.Fatalf("report %d started after the one before it", i)
		}
	}
	for _, job := range []store.Job{ready, failed, pending} {
		if _, ok := position[job.Session]; !ok {
			t.Fatalf("session %s is missing from the list", job.Session)
		}
	}
	if position[pending.Session] > position[failed.Session] || position[failed.Session] > position[ready.Session] {
		t.Fatalf("positions pending=%d failed=%d ready=%d, want newest first", position[pending.Session], position[failed.Session], position[ready.Session])
	}

	got := everything[position[failed.Session]]
	if got.Status != store.Failed || got.Err != "uci: engine exited" || got.Duration != 30*time.Millisecond || got.CompletedAt == nil ||
		got.Moves != 5 || got.Depth != 12 || !got.FinishedAt.Equal(failed.FinishedAt) || len(got.Positions) != 0 {
		t.Fatalf("failed report = %+v", got)
	}
	if got := everything[position[pending.Session]]; got.Status != store.Pending || got.CompletedAt != nil || got.Duration != 0 {
		t.Fatalf("pending report = %+v", got)
	}

	for _, tt := range []struct {
		status store.Status
		mine   uuid.UUID
	}{{store.Ready, ready.Session}, {store.Failed, failed.Session}, {store.Pending, pending.Session}} {
		found := false
		for _, r := range collect(tt.status) {
			if r.Status != tt.status {
				t.Fatalf("a %s report in the %s list", r.Status, tt.status)
			}
			found = found || r.Session == tt.mine
		}
		if !found {
			t.Fatalf("session %s is missing from the %s list", tt.mine, tt.status)
		}
	}

	if full, err := reports.Get(ctx, ready.Session); err != nil || !full.FinishedAt.Equal(ready.FinishedAt) {
		t.Fatalf("Get = %+v, %v; want the game finish time", full, err)
	}
	if _, _, err := reports.List(ctx, store.ReportFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}
}

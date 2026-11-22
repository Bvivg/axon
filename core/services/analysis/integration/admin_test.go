//go:build integration

package integration

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/analysis/internal/store"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
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
			page, err := reports.List(ctx, store.ReportFilter{Status: status, Cursor: cursor, Limit: 2})
			if err != nil {
				t.Fatalf("List(%q): %v", status, err)
			}
			if len(page.Reports) > 2 || page.Total != nil {
				t.Fatalf("a page of %d reports with total %v, want at most 2 and no total", len(page.Reports), page.Total)
			}
			all = append(all, page.Reports...)
			if page.Next == "" {
				return all
			}
			cursor = page.Next
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
	if _, err := reports.List(ctx, store.ReportFilter{Cursor: "%%%", Limit: 10}); !errors.Is(err, store.ErrInvalidCursor) {
		t.Fatalf("broken cursor = %v", err)
	}

	mine := "(session = " + ready.Session.String() + " OR session = " + failed.Session.String() + " OR session = " + pending.Session.String() + ")"
	done, err := reports.List(ctx, store.ReportFilter{Limit: 10, List: adminquery.Request{Filter: mine + " AND status != pending"}})
	if err != nil || len(done.Reports) != 2 || done.Next != "" || done.Reports[0].Session != failed.Session || done.Reports[1].Session != ready.Session {
		t.Fatalf("finished reports by cursor = %+v, %v", done, err)
	}
	offset := int32(1)
	paged, err := reports.List(ctx, store.ReportFilter{List: adminquery.Request{
		Filter: mine + " AND started >= 2000-01-01", OrderBy: "started", Offset: &offset, PageSize: 1,
	}})
	if err != nil || paged.Total == nil || *paged.Total != 3 || paged.Next != "" || len(paged.Reports) != 1 || paged.Reports[0].Session != failed.Session {
		t.Fatalf("offset page = %+v, %v", paged, err)
	}
	completed, err := reports.List(ctx, store.ReportFilter{Status: store.Ready, List: adminquery.Request{Filter: mine + " AND finished >= 2000-01-01", OrderBy: "finished desc", Offset: new(int32)}})
	if err != nil || *completed.Total != 1 || completed.Reports[0].Session != ready.Session {
		t.Fatalf("completed ready reports = %+v, %v", completed, err)
	}
	var invalid *adminquery.Error
	if _, err := reports.List(ctx, store.ReportFilter{Limit: 10, List: adminquery.Request{OrderBy: "started"}}); !errors.As(err, &invalid) || invalid.Field != "order_by" {
		t.Fatalf("order without offset = %v", err)
	}
}

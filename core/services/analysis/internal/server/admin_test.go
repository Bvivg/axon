package server_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/analysis/internal/server"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type listing struct {
	reports []store.Report
	total   *int64
	err     error
	asked   *store.ReportFilter
}

func (l *listing) List(_ context.Context, f store.ReportFilter) (store.ReportPage, error) {
	l.asked = &f
	if l.err != nil {
		return store.ReportPage{}, l.err
	}
	return store.ReportPage{Reports: l.reports, Next: "page-2", Total: l.total}, nil
}

func TestListAnalysisReports(t *testing.T) {
	finished := time.Date(2026, 11, 20, 11, 59, 0, 0, time.UTC)
	started := finished.Add(time.Minute)
	completed := started.Add(1500 * time.Millisecond)
	failed := store.Report{
		Session: uuid.New(), Status: store.Failed, Depth: 12, Engine: "Stockfish 17.1", Moves: 40,
		FinishedAt: finished, StartedAt: started, CompletedAt: &completed, Duration: 1500 * time.Millisecond, Err: "uci: engine exited",
	}
	pending := store.Report{Session: uuid.New(), Status: store.Pending, Depth: 12, Engine: "Stockfish 17.1", Moves: 3, FinishedAt: finished, StartedAt: started}
	total, offset := int64(31), int32(10)
	invalid := &adminquery.Error{Field: "order_by", Message: "needs offset paging"}

	tests := []struct {
		name    string
		request *analysisv1.ListAnalysisReportsRequest
		listing *listing
		code    connect.Code
		asked   *store.ReportFilter
		want    []*analysisv1.AnalysisReportEntry
	}{
		{
			name:    "a failed report keeps its error and timings",
			request: &analysisv1.ListAnalysisReportsRequest{Status: "failed", Cursor: "page-1", PageSize: 10},
			listing: &listing{reports: []store.Report{failed}},
			asked:   &store.ReportFilter{Status: store.Failed, Cursor: "page-1", Limit: 10, List: adminquery.Request{PageSize: 10}},
			want: []*analysisv1.AnalysisReportEntry{{
				SessionId: failed.Session.String(), Status: "failed", Engine: "Stockfish 17.1", Depth: 12, Moves: 40, DurationMs: 1500, Error: "uci: engine exited",
			}},
		},
		{
			name:    "a pending report has no completion yet",
			request: &analysisv1.ListAnalysisReportsRequest{},
			listing: &listing{reports: []store.Report{pending}},
			asked:   &store.ReportFilter{Limit: server.DefaultLimit},
			want:    []*analysisv1.AnalysisReportEntry{{SessionId: pending.Session.String(), Status: "pending", Engine: "Stockfish 17.1", Depth: 12, Moves: 3}},
		},
		{
			name:    "a page too large is capped",
			request: &analysisv1.ListAnalysisReportsRequest{Status: "ready", PageSize: 5000},
			listing: &listing{},
			asked:   &store.ReportFilter{Status: store.Ready, Limit: server.MaxLimit, List: adminquery.Request{PageSize: 5000}},
			want:    []*analysisv1.AnalysisReportEntry{},
		},
		{
			name:    "an offset page carries the query and the total",
			request: &analysisv1.ListAnalysisReportsRequest{Filter: proto.String("status = failed"), OrderBy: proto.String("finished desc"), Offset: &offset, PageSize: 10},
			listing: &listing{reports: []store.Report{failed}, total: &total},
			asked:   &store.ReportFilter{Limit: 10, List: adminquery.Request{Filter: "status = failed", OrderBy: "finished desc", Offset: &offset, PageSize: 10}},
			want: []*analysisv1.AnalysisReportEntry{{
				SessionId: failed.Session.String(), Status: "failed", Engine: "Stockfish 17.1", Depth: 12, Moves: 40, DurationMs: 1500, Error: "uci: engine exited",
			}},
		},
		{
			name:    "an invalid query",
			request: &analysisv1.ListAnalysisReportsRequest{OrderBy: proto.String("started")},
			listing: &listing{err: invalid},
			code:    connect.CodeInvalidArgument,
			asked:   &store.ReportFilter{Limit: server.DefaultLimit, List: adminquery.Request{OrderBy: "started"}},
		},
		{name: "an unknown status", request: &analysisv1.ListAnalysisReportsRequest{Status: "done"}, listing: &listing{}, code: connect.CodeInvalidArgument},
		{
			name:    "a broken cursor",
			request: &analysisv1.ListAnalysisReportsRequest{Cursor: "%%%"},
			listing: &listing{err: store.ErrInvalidCursor},
			code:    connect.CodeInvalidArgument,
			asked:   &store.ReportFilter{Cursor: "%%%", Limit: server.DefaultLimit},
		},
		{
			name:    "a store failure",
			request: &analysisv1.ListAnalysisReportsRequest{},
			listing: &listing{err: errors.New("connection refused")},
			code:    connect.CodeUnavailable,
			asked:   &store.ReportFilter{Limit: server.DefaultLimit},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := server.NewAdmin(tt.listing, slog.New(slog.DiscardHandler)).ListAnalysisReports(t.Context(), connect.NewRequest(tt.request))
			switch {
			case tt.code == 0 && err != nil:
				t.Fatalf("ListAnalysisReports: %v", err)
			case tt.code != 0 && connect.CodeOf(err) != tt.code:
				t.Fatalf("code = %v (%v), want %v", connect.CodeOf(err), err, tt.code)
			}
			if (tt.listing.asked == nil) != (tt.asked == nil) || (tt.asked != nil && *tt.listing.asked != *tt.asked) {
				t.Errorf("store asked %+v, want %+v", tt.listing.asked, tt.asked)
			}
			if tt.code != 0 {
				return
			}
			if (got.Msg.Total == nil) != (tt.listing.total == nil) || tt.listing.total != nil && got.Msg.GetTotal() != *tt.listing.total {
				t.Fatalf("total = %v, want %v", got.Msg.Total, tt.listing.total)
			}
			if got.Msg.GetNextCursor() != "page-2" || len(got.Msg.GetReports()) != len(tt.want) {
				t.Fatalf("response = %v", got.Msg)
			}
			for i, want := range tt.want {
				have := got.Msg.GetReports()[i]
				if have.GetSessionId() != want.GetSessionId() || have.GetStatus() != want.GetStatus() || have.GetEngine() != want.GetEngine() ||
					have.GetDepth() != want.GetDepth() || have.GetMoves() != want.GetMoves() || have.GetDurationMs() != want.GetDurationMs() ||
					have.GetError() != want.GetError() || !have.GetGameFinishedAt().AsTime().Equal(finished) || !have.GetStartedAt().AsTime().Equal(started) {
					t.Errorf("report %d = %v, want %v", i, have, want)
				}
				if done := tt.listing.reports[i].CompletedAt; (done == nil) != (have.CompletedAt == nil) || (done != nil && !have.GetCompletedAt().AsTime().Equal(*done)) {
					t.Errorf("report %d completed_at = %v, want %v", i, have.GetCompletedAt(), done)
				}
			}
		})
	}
}

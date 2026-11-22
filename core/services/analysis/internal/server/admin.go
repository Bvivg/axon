package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/analysis/internal/store"
	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

const (
	DefaultLimit = 25
	MaxLimit     = 100
)

var AdminRules = authz.Rules{
	analysisv1connect.AnalysisAdminServiceListAnalysisReportsProcedure: authz.GamesRead,
}

type Listing interface {
	List(ctx context.Context, f store.ReportFilter) ([]store.Report, string, error)
}

type Admin struct {
	reports Listing
	log     *slog.Logger
}

var _ analysisv1connect.AnalysisAdminServiceHandler = (*Admin)(nil)

func NewAdmin(reports Listing, log *slog.Logger) *Admin {
	return &Admin{reports: reports, log: log}
}

var (
	errStatus = errors.New("status must be pending, ready or failed")
	errCursor = errors.New("cursor is not valid")
)

func (a *Admin) ListAnalysisReports(ctx context.Context, req *connect.Request[analysisv1.ListAnalysisReportsRequest]) (*connect.Response[analysisv1.ListAnalysisReportsResponse], error) {
	filter := store.ReportFilter{Status: store.Status(req.Msg.GetStatus()), Cursor: req.Msg.GetCursor(), Limit: int(req.Msg.GetPageSize())}
	if filter.Status != "" && !filter.Status.Valid() {
		return nil, connect.NewError(connect.CodeInvalidArgument, errStatus)
	}
	switch {
	case filter.Limit <= 0:
		filter.Limit = DefaultLimit
	case filter.Limit > MaxLimit:
		filter.Limit = MaxLimit
	}
	reports, next, err := a.reports.List(ctx, filter)
	if errors.Is(err, store.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errCursor)
	}
	if err != nil {
		a.log.ErrorContext(ctx, "analysis reports read failed", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	out := &analysisv1.ListAnalysisReportsResponse{NextCursor: next, Reports: make([]*analysisv1.AnalysisReportEntry, len(reports))}
	for i, r := range reports {
		out.Reports[i] = entry(r)
	}
	return connect.NewResponse(out), nil
}

func entry(r store.Report) *analysisv1.AnalysisReportEntry {
	out := &analysisv1.AnalysisReportEntry{
		SessionId:      r.Session.String(),
		Status:         string(r.Status),
		Engine:         r.Engine,
		Depth:          int32(r.Depth),
		Moves:          int32(r.Moves),
		GameFinishedAt: timestamppb.New(r.FinishedAt),
		StartedAt:      timestamppb.New(r.StartedAt),
		DurationMs:     int32(r.Duration.Milliseconds()),
		Error:          r.Err,
	}
	if r.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*r.CompletedAt)
	}
	return out
}

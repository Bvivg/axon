package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type Reports interface {
	Get(ctx context.Context, session uuid.UUID) (store.Report, error)
}

type Verifier interface {
	Verify(raw string) (authn.Claims, error)
}

type Handler struct {
	reports  Reports
	verifier Verifier
	log      *slog.Logger
}

var _ analysisv1connect.AnalysisServiceHandler = (*Handler)(nil)

func New(reports Reports, verifier Verifier, log *slog.Logger) *Handler {
	return &Handler{reports: reports, verifier: verifier, log: log}
}

var (
	errSignIn      = errors.New("sign in to read game analysis")
	errSession     = errors.New("session_id must be a UUID")
	errNotAnalysed = errors.New("this game has no analysis")
	errUnavailable = errors.New("analysis is unavailable")
)

func (h *Handler) authenticate(headers http.Header) error {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return connect.NewError(connect.CodeUnauthenticated, errSignIn)
	}
	if _, err := h.verifier.Verify(raw); err != nil {
		return connect.NewError(connect.CodeUnauthenticated, errSignIn)
	}
	return nil
}

func (h *Handler) GetAnalysis(ctx context.Context, req *connect.Request[analysisv1.GetAnalysisRequest]) (*connect.Response[analysisv1.GetAnalysisResponse], error) {
	if err := h.authenticate(req.Header()); err != nil {
		return nil, err
	}
	session, err := uuid.Parse(req.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errSession)
	}
	report, err := h.reports.Get(ctx, session)
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errNotAnalysed)
	}
	if err != nil {
		h.log.ErrorContext(ctx, "analysis read failed", "session_id", session, "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	return connect.NewResponse(Response(report)), nil
}

var statuses = map[store.Status]analysisv1.AnalysisStatus{
	store.Pending: analysisv1.AnalysisStatus_ANALYSIS_STATUS_PENDING,
	store.Ready:   analysisv1.AnalysisStatus_ANALYSIS_STATUS_READY,
	store.Failed:  analysisv1.AnalysisStatus_ANALYSIS_STATUS_FAILED,
}

var classes = map[review.Class]analysisv1.MoveClass{
	review.Best:       analysisv1.MoveClass_MOVE_CLASS_BEST,
	review.Good:       analysisv1.MoveClass_MOVE_CLASS_GOOD,
	review.Inaccuracy: analysisv1.MoveClass_MOVE_CLASS_INACCURACY,
	review.Mistake:    analysisv1.MoveClass_MOVE_CLASS_MISTAKE,
	review.Blunder:    analysisv1.MoveClass_MOVE_CLASS_BLUNDER,
}

func Response(report store.Report) *analysisv1.GetAnalysisResponse {
	out := &analysisv1.GetAnalysisResponse{
		SessionId: report.Session.String(),
		Status:    statuses[report.Status],
		Depth:     uint32(report.Depth),
		Engine:    report.Engine,
	}
	if report.CompletedAt != nil {
		out.CompletedAt = timestamppb.New(*report.CompletedAt)
	}
	if report.Status != store.Ready {
		return out
	}
	out.Positions = make([]*analysisv1.PositionAnalysis, len(report.Positions))
	for i, p := range report.Positions {
		position := &analysisv1.PositionAnalysis{Ply: uint32(p.Ply), Centipawns: int32(p.Centipawns), Loss: uint32(p.Loss), MoveClass: classes[p.Class]}
		if p.Mate != nil {
			position.Mate = new(int32(*p.Mate))
		}
		if p.BestMove != "" {
			position.BestMove = new(p.BestMove)
		}
		if p.Played != "" {
			position.PlayedMove = new(p.Played)
		}
		out.Positions[i] = position
	}
	return out
}

package server_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/server"
	"github.com/bvivg/axon/core/services/analysis/internal/store"
	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type reports map[uuid.UUID]store.Report

func (r reports) Get(_ context.Context, session uuid.UUID) (store.Report, error) {
	if session == broken {
		return store.Report{}, errors.New("connection refused")
	}
	report, ok := r[session]
	if !ok {
		return store.Report{}, store.ErrNotFound
	}
	return report, nil
}

type verifier struct{}

func (verifier) Verify(raw string) (authn.Claims, error) {
	if raw != "good" {
		return authn.Claims{}, authn.ErrInvalidToken
	}
	return authn.Claims{UserID: uuid.New()}, nil
}

var broken = uuid.New()

func request(token, session string) *connect.Request[analysisv1.GetAnalysisRequest] {
	req := connect.NewRequest(&analysisv1.GetAnalysisRequest{SessionId: session})
	if token != "" {
		req.Header().Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestGetAnalysis(t *testing.T) {
	ready, pending, failed := uuid.New(), uuid.New(), uuid.New()
	done := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	h := server.New(reports{
		ready: {Session: ready, Status: store.Ready, Depth: 12, Engine: "Stockfish 17.1", CompletedAt: &done, Positions: []review.Position{
			{Ply: 0, Centipawns: 25, BestMove: "e2e4", Class: review.ClassNone},
			{Ply: 1, Centipawns: 10000, Mate: new(1), BestMove: "h5f7", Played: "g8f6", Loss: 10038, Class: review.Blunder},
			{Ply: 2, Centipawns: 10000, Mate: new(0), Played: "h5f7", Class: review.Best},
		}},
		pending: {Session: pending, Status: store.Pending, Depth: 12, Engine: "Stockfish 17.1"},
		failed:  {Session: failed, Status: store.Failed, Depth: 12, Engine: "Stockfish 17.1", CompletedAt: &done, Err: "uci: engine exited"},
	}, verifier{}, slog.New(slog.DiscardHandler))

	got, err := h.GetAnalysis(t.Context(), request("good", ready.String()))
	if err != nil {
		t.Fatal(err)
	}
	msg := got.Msg
	if msg.GetStatus() != analysisv1.AnalysisStatus_ANALYSIS_STATUS_READY || msg.GetDepth() != 12 || msg.GetEngine() != "Stockfish 17.1" ||
		!msg.GetCompletedAt().AsTime().Equal(done) || len(msg.GetPositions()) != 3 {
		t.Fatalf("ready = %v", msg)
	}
	start, blunder, mate := msg.GetPositions()[0], msg.GetPositions()[1], msg.GetPositions()[2]
	if start.PlayedMove != nil || start.Mate != nil || start.GetBestMove() != "e2e4" || start.GetMoveClass() != analysisv1.MoveClass_MOVE_CLASS_UNSPECIFIED {
		t.Fatalf("start = %v", start)
	}
	if blunder.GetMoveClass() != analysisv1.MoveClass_MOVE_CLASS_BLUNDER || blunder.GetLoss() != 10038 || blunder.GetPlayedMove() != "g8f6" || blunder.GetMate() != 1 {
		t.Fatalf("blunder = %v", blunder)
	}
	if mate.Mate == nil || mate.GetMate() != 0 || mate.BestMove != nil || mate.GetCentipawns() != 10000 {
		t.Fatalf("mate = %v", mate)
	}

	for name, tt := range map[string]struct {
		session uuid.UUID
		status  analysisv1.AnalysisStatus
	}{"pending": {pending, analysisv1.AnalysisStatus_ANALYSIS_STATUS_PENDING}, "failed": {failed, analysisv1.AnalysisStatus_ANALYSIS_STATUS_FAILED}} {
		got, err := h.GetAnalysis(t.Context(), request("good", tt.session.String()))
		if err != nil || got.Msg.GetStatus() != tt.status || len(got.Msg.GetPositions()) != 0 {
			t.Fatalf("%s = %v, %v", name, got, err)
		}
	}

	for name, tt := range map[string]struct {
		token, session string
		code           connect.Code
	}{
		"no token":      {"", ready.String(), connect.CodeUnauthenticated},
		"bad token":     {"forged", ready.String(), connect.CodeUnauthenticated},
		"bad session":   {"good", "chess", connect.CodeInvalidArgument},
		"not analysed":  {"good", uuid.NewString(), connect.CodeNotFound},
		"store failure": {"good", broken.String(), connect.CodeUnavailable},
	} {
		if _, err := h.GetAnalysis(t.Context(), request(tt.token, tt.session)); connect.CodeOf(err) != tt.code {
			t.Errorf("%s: code = %v, want %v", name, connect.CodeOf(err), tt.code)
		}
	}
}

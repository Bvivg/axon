package projection

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const (
	FinishedTopic = "game.finished.v1"
	AnalysisTopic = "game.analysis_requested.v1"
	OutboxTable   = "game.outbox"
)

var (
	finishedNamespace = uuid.MustParse("7af34790-8eb2-4832-86b6-7d161fbf75e4")
	analysisNamespace = uuid.MustParse("b4098f8d-4441-4a52-b69c-1a7c1ef671b4")
)

func FinishedEventID(id uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(finishedNamespace, id[:])
}

func AnalysisEventID(id uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(analysisNamespace, id[:])
}

func finished(game session.Session) bool {
	return game.Opponent != nil && game.State.Over()
}

func gameFinished(game session.Session, id uuid.UUID, at time.Time) *eventsv1.GameFinished {
	players := make([]string, 0, len(game.Players()))
	for _, player := range game.Players() {
		players = append(players, player.String())
	}
	payload := &eventsv1.GameFinished{
		EventId:    id.String(),
		SessionId:  game.ID.String(),
		Game:       string(game.Game),
		Players:    players,
		Outcome:    eventsv1.GameFinished_OUTCOME_WIN,
		WinnerSeat: int32(game.State.Winner),
		FinishedAt: timestamppb.New(at.UTC()),
		Rated:      game.Rated,
		Plies:      proto.Uint32(uint32(game.State.Ply)),
	}
	if game.Tournament != nil {
		payload.TournamentId = proto.String(game.Tournament.String())
	}
	if game.State.Status == engine.StatusDraw || game.State.Winner < 0 {
		payload.Outcome, payload.WinnerSeat = eventsv1.GameFinished_OUTCOME_DRAW, int32(engine.NoSeat)
	}
	if game.Multiseat() || len(game.Left) > 0 {
		for _, place := range game.Places() {
			payload.Places = append(payload.Places, uint32(place))
		}
		if slices.Contains(payload.Places, 1) && slices.ContainsFunc(payload.Places, func(place uint32) bool { return place > 1 }) {
			payload.Outcome = eventsv1.GameFinished_OUTCOME_WIN
			payload.WinnerSeat = int32(slices.Index(payload.Places, 1))
		}
	}
	return payload
}

func FinishedEvent(game session.Session, at time.Time) (outbox.Event, bool) {
	if !finished(game) {
		return outbox.Event{}, false
	}
	id := FinishedEventID(game.ID)
	return outbox.Event{ID: id, Topic: FinishedTopic, AggregateID: string(game.Game), Payload: gameFinished(game, id, at)}, true
}

func AnalysisEvent(game session.Session, at time.Time) (outbox.Event, bool) {
	if !finished(game) || !game.Rated || game.Game != chess.Key {
		return outbox.Event{}, false
	}
	moves, err := chess.Moves(game.State)
	if err != nil || len(moves) == 0 {
		return outbox.Event{}, false
	}
	id := AnalysisEventID(game.ID)
	payload := gameFinished(game, id, at)
	payload.Moves = moves
	return outbox.Event{ID: id, Topic: AnalysisTopic, AggregateID: game.ID.String(), Payload: payload}, true
}

func (m *Postgres) PruneOutbox(ctx context.Context, every, olderThan time.Duration, log *slog.Logger) {
	for {
		removed, err := outbox.Cleanup(ctx, m.pool, OutboxTable, olderThan)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.WarnContext(ctx, "outbox cleanup failed", "error", err)
		case removed > 0:
			log.InfoContext(ctx, "outbox rows removed", "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

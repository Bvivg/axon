package projection

import (
	"context"
	"log/slog"
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
	OutboxTable   = "game.outbox"
)

var finishedNamespace = uuid.MustParse("7af34790-8eb2-4832-86b6-7d161fbf75e4")

func FinishedEventID(id uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(finishedNamespace, id[:])
}

func finished(game session.Session) bool {
	return game.Opponent != nil && game.State.Over()
}

func FinishedEvent(game session.Session, at time.Time) (outbox.Event, bool) {
	if !finished(game) {
		return outbox.Event{}, false
	}
	id := FinishedEventID(game.ID)
	payload := &eventsv1.GameFinished{
		EventId:    id.String(),
		SessionId:  game.ID.String(),
		Game:       string(game.Game),
		Players:    []string{game.Creator.String(), game.Opponent.String()},
		Outcome:    eventsv1.GameFinished_OUTCOME_WIN,
		WinnerSeat: int32(game.State.Winner),
		FinishedAt: timestamppb.New(at.UTC()),
		Rated:      game.Rated,
		Plies:      proto.Uint32(uint32(game.State.Ply)),
	}
	if game.Tournament != nil {
		payload.TournamentId = proto.String(game.Tournament.String())
	}
	if game.Game == chess.Key {
		if moves, err := chess.Moves(game.State); err == nil {
			payload.Moves = moves
		}
	}
	if game.State.Status == engine.StatusDraw || game.State.Winner < 0 {
		payload.Outcome, payload.WinnerSeat = eventsv1.GameFinished_OUTCOME_DRAW, int32(engine.NoSeat)
	}
	return outbox.Event{ID: id, Topic: FinishedTopic, AggregateID: string(game.Game), Payload: payload}, true
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

package consumer

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/tournament/internal/orchestrator"
	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

const (
	FinishedTopic = "game.finished.v1"
	RatingTopic   = "rating.changed.v1"
	Group         = "tournament"
)

var (
	ErrForeign = errors.New("consumer: not a tournament game")
	ErrInvalid = errors.New("consumer: invalid event")
)

var (
	finishedType = string((&eventsv1.GameFinished{}).ProtoReflect().Descriptor().FullName())
	ratingType   = string((&eventsv1.RatingChanged{}).ProtoReflect().Descriptor().FullName())
)

func parse(raw, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s %q", ErrInvalid, what, raw)
	}
	return id, nil
}

func DecodeFinished(value []byte) (orchestrator.Result, error) {
	event := &eventsv1.GameFinished{}
	if err := proto.Unmarshal(value, event); err != nil {
		return orchestrator.Result{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if event.TournamentId == nil {
		return orchestrator.Result{}, ErrForeign
	}
	var out orchestrator.Result
	var err error
	if out.Tournament, err = parse(event.GetTournamentId(), "tournament"); err != nil {
		return orchestrator.Result{}, err
	}
	if out.EventID, err = parse(event.GetEventId(), "event"); err != nil {
		return orchestrator.Result{}, err
	}
	if out.Session, err = parse(event.GetSessionId(), "session"); err != nil {
		return orchestrator.Result{}, err
	}
	if len(event.GetPlayers()) != 2 {
		return orchestrator.Result{}, fmt.Errorf("%w: %d players", ErrInvalid, len(event.GetPlayers()))
	}
	for seat, raw := range event.GetPlayers() {
		if out.Players[seat], err = parse(raw, "player"); err != nil {
			return orchestrator.Result{}, err
		}
	}
	switch event.GetOutcome() {
	case eventsv1.GameFinished_OUTCOME_WIN:
		if seat := event.GetWinnerSeat(); seat != 0 && seat != 1 {
			return orchestrator.Result{}, fmt.Errorf("%w: winner seat %d", ErrInvalid, seat)
		}
		out.Winner = int(event.GetWinnerSeat())
	case eventsv1.GameFinished_OUTCOME_DRAW:
		out.Winner = saga.NoWinner
	default:
		return orchestrator.Result{}, fmt.Errorf("%w: outcome %v", ErrInvalid, event.GetOutcome())
	}
	if !event.GetFinishedAt().IsValid() {
		return orchestrator.Result{}, fmt.Errorf("%w: finished_at is missing", ErrInvalid)
	}
	out.FinishedAt = event.GetFinishedAt().AsTime().UTC()
	out.Plies = 2
	if event.Plies != nil {
		out.Plies = int(event.GetPlies())
	}
	return out, nil
}

type Ratings struct {
	Game    string
	At      time.Time
	Ratings map[uuid.UUID]int
}

func DecodeRatings(value []byte) (Ratings, error) {
	event := &eventsv1.RatingChanged{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Ratings{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if event.GetGame() == "" || !event.GetChangedAt().IsValid() {
		return Ratings{}, fmt.Errorf("%w: rating change without a game or time", ErrInvalid)
	}
	out := Ratings{Game: event.GetGame(), At: event.GetChangedAt().AsTime().UTC(), Ratings: map[uuid.UUID]int{}}
	for _, change := range event.GetChanges() {
		user, err := parse(change.GetUserId(), "user")
		if err != nil {
			return Ratings{}, err
		}
		out.Ratings[user] = int(change.GetAfter())
	}
	return out, nil
}

package consumer

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

const (
	FinishedTopic = "game.finished.v1"
	Chess         = "chess"
)

var (
	ErrForeign  = errors.New("consumer: not a game.finished event")
	ErrInvalid  = errors.New("consumer: invalid game.finished event")
	ErrNotChess = errors.New("consumer: not a chess game")
)

var finishedType = string((&eventsv1.GameFinished{}).ProtoReflect().Descriptor().FullName())

type Game struct {
	EventID    uuid.UUID
	Session    uuid.UUID
	Moves      []string
	FinishedAt time.Time
}

func Decode(kind string, value []byte) (Game, error) {
	if kind != "" && kind != finishedType {
		return Game{}, fmt.Errorf("%w: %s", ErrForeign, kind)
	}
	event := &eventsv1.GameFinished{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Game{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if event.GetGame() != Chess {
		return Game{}, fmt.Errorf("%w: %q", ErrNotChess, event.GetGame())
	}
	eventID, err := uuid.Parse(event.GetEventId())
	if err != nil {
		return Game{}, fmt.Errorf("%w: event id %q", ErrInvalid, event.GetEventId())
	}
	session, err := uuid.Parse(event.GetSessionId())
	if err != nil {
		return Game{}, fmt.Errorf("%w: session %q", ErrInvalid, event.GetSessionId())
	}
	if !event.GetFinishedAt().IsValid() {
		return Game{}, fmt.Errorf("%w: finished_at is missing", ErrInvalid)
	}
	return Game{EventID: eventID, Session: session, Moves: event.GetMoves(), FinishedAt: event.GetFinishedAt().AsTime().UTC()}, nil
}

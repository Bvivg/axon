package session

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type DrawAction int

const (
	OfferDraw DrawAction = iota + 1
	AcceptDraw
	DeclineDraw
)

func (s *Store) Draw(ctx context.Context, id, user uuid.UUID, action DrawAction) (Session, error) {
	return s.change(ctx, id, func(tx pgx.Tx, game Session) (Session, error) {
		if !game.HasPlayer(user) {
			return Session{}, ErrNotFound
		}
		if game.Opponent == nil || game.State.Over() {
			return Session{}, ErrNotPlaying
		}
		now := time.Now()
		if game.expire(now) {
			return s.persist(ctx, tx, game)
		}
		pending := game.DrawOffer != nil && *game.DrawOffer != user
		switch {
		case action == AcceptDraw && pending, action == OfferDraw && pending:
			game.Clock = game.ClockAt(now)
			game.State = game.State.Finish(engine.NoSeat)
			game.AgreedDraw, game.DrawOffer = true, nil
		case action == OfferDraw:
			if game.DrawOffer != nil {
				return game, nil
			}
			game.Clock = game.ClockAt(now)
			game.DrawOffer = &user
		case action == DeclineDraw && pending:
			game.Clock = game.ClockAt(now)
			game.DrawOffer = nil
		default:
			return Session{}, ErrNoDrawOffer
		}
		return s.persist(ctx, tx, game)
	})
}

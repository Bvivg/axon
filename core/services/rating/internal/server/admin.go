package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

var AdminRules = authz.Rules{
	ratingv1connect.RatingAdminServiceListRatingChangesProcedure: authz.GamesRead,
	ratingv1connect.RatingAdminServiceListRatingsProcedure:       authz.GamesRead,
}

type AdminStore interface {
	Changes(ctx context.Context, f store.ChangeFilter) ([]store.Change, string, error)
	Ratings(ctx context.Context, f store.RatingFilter) ([]store.RatingRow, string, error)
}

type Admin struct {
	store AdminStore
	log   *slog.Logger
}

var _ ratingv1connect.RatingAdminServiceHandler = (*Admin)(nil)

func NewAdmin(store AdminStore, log *slog.Logger) *Admin {
	return &Admin{store: store, log: log}
}

func adminFilter(game, user string, limit int32) (string, *uuid.UUID, int, error) {
	if game != "" && !ledger.ValidGame(game) {
		return "", nil, 0, connect.NewError(connect.CodeInvalidArgument, errors.New("game must be a known game key"))
	}
	var id *uuid.UUID
	if user != "" {
		parsed, err := uuid.Parse(user)
		if err != nil {
			return "", nil, 0, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id is not valid"))
		}
		id = &parsed
	}
	size := int(limit)
	switch {
	case size <= 0:
		size = DefaultLimit
	case size > MaxLimit:
		size = MaxLimit
	}
	return game, id, size, nil
}

func (a *Admin) ListRatingChanges(ctx context.Context, req *connect.Request[ratingv1.ListRatingChangesRequest]) (*connect.Response[ratingv1.ListRatingChangesResponse], error) {
	game, user, limit, err := adminFilter(req.Msg.GetGame(), req.Msg.GetUserId(), req.Msg.GetPageSize())
	if err != nil {
		return nil, err
	}
	changes, next, err := a.store.Changes(ctx, store.ChangeFilter{Game: game, User: user, Cursor: req.Msg.GetCursor(), Limit: limit})
	if errors.Is(err, store.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cursor is not valid"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "rating changes read failed", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	out := &ratingv1.ListRatingChangesResponse{NextCursor: next, Changes: make([]*ratingv1.RatingChangeEntry, len(changes))}
	for i, c := range changes {
		out.Changes[i] = &ratingv1.RatingChangeEntry{
			SessionId: c.Session.String(), UserId: c.User.String(), Game: c.Game,
			RatingBefore: c.RatingBefore, RatingAfter: c.RatingAfter, DeviationAfter: c.DeviationAfter,
			FinishedAt: timestamppb.New(c.FinishedAt),
		}
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) ListRatings(ctx context.Context, req *connect.Request[ratingv1.ListRatingsRequest]) (*connect.Response[ratingv1.ListRatingsResponse], error) {
	game, user, limit, err := adminFilter(req.Msg.GetGame(), req.Msg.GetUserId(), req.Msg.GetPageSize())
	if err != nil {
		return nil, err
	}
	ratings, next, err := a.store.Ratings(ctx, store.RatingFilter{Game: game, User: user, Cursor: req.Msg.GetCursor(), Limit: limit})
	if errors.Is(err, store.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cursor is not valid"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "ratings read failed", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	out := &ratingv1.ListRatingsResponse{NextCursor: next, Ratings: make([]*ratingv1.RatingEntry, len(ratings))}
	for i, r := range ratings {
		p := r.Player
		out.Ratings[i] = &ratingv1.RatingEntry{
			UserId: p.User.String(), Game: r.Game, Rating: p.Rating.Rating, Deviation: p.Rating.Deviation, Volatility: p.Rating.Volatility,
			Games: int32(p.Games), Wins: int32(p.Wins), Losses: int32(p.Losses), Draws: int32(p.Draws), UpdatedAt: timestamppb.New(r.UpdatedAt),
		}
	}
	return connect.NewResponse(out), nil
}

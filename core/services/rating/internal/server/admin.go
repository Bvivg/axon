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
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/authz"
)

var AdminRules = authz.Rules{
	ratingv1connect.RatingAdminServiceListRatingChangesProcedure: authz.GamesRead,
	ratingv1connect.RatingAdminServiceListRatingsProcedure:       authz.GamesRead,
}

type AdminStore interface {
	Changes(ctx context.Context, f store.ChangeFilter) (store.ChangePage, error)
	Ratings(ctx context.Context, f store.RatingFilter) (store.RatingPage, error)
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

func invalidQuery(err error) error {
	var invalid *adminquery.Error
	if errors.As(err, &invalid) {
		return admincrud.QueryError(err)
	}
	return nil
}

func (a *Admin) ListRatingChanges(ctx context.Context, req *connect.Request[ratingv1.ListRatingChangesRequest]) (*connect.Response[ratingv1.ListRatingChangesResponse], error) {
	game, user, limit, err := adminFilter(req.Msg.GetGame(), req.Msg.GetUserId(), req.Msg.GetPageSize())
	if err != nil {
		return nil, err
	}
	page, err := a.store.Changes(ctx, store.ChangeFilter{
		Game: game, User: user, Cursor: req.Msg.GetCursor(), Limit: limit,
		List: adminquery.Request{Filter: req.Msg.GetFilter(), OrderBy: req.Msg.GetOrderBy(), Offset: req.Msg.Offset, PageSize: req.Msg.GetPageSize()},
	})
	if invalid := invalidQuery(err); invalid != nil {
		return nil, invalid
	}
	if errors.Is(err, store.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cursor is not valid"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "rating changes read failed", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	out := &ratingv1.ListRatingChangesResponse{NextCursor: page.Next, Total: page.Total, Changes: make([]*ratingv1.RatingChangeEntry, len(page.Changes))}
	for i, c := range page.Changes {
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
	page, err := a.store.Ratings(ctx, store.RatingFilter{
		Game: game, User: user, Cursor: req.Msg.GetCursor(), Limit: limit,
		List: adminquery.Request{Filter: req.Msg.GetFilter(), OrderBy: req.Msg.GetOrderBy(), Offset: req.Msg.Offset, PageSize: req.Msg.GetPageSize()},
	})
	if invalid := invalidQuery(err); invalid != nil {
		return nil, invalid
	}
	if errors.Is(err, store.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cursor is not valid"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "ratings read failed", "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	out := &ratingv1.ListRatingsResponse{NextCursor: page.Next, Total: page.Total, Ratings: make([]*ratingv1.RatingEntry, len(page.Ratings))}
	for i, r := range page.Ratings {
		p := r.Player
		out.Ratings[i] = &ratingv1.RatingEntry{
			UserId: p.User.String(), Game: r.Game, Rating: p.Rating.Rating, Deviation: p.Rating.Deviation, Volatility: p.Rating.Volatility,
			Games: int32(p.Games), Wins: int32(p.Wins), Losses: int32(p.Losses), Draws: int32(p.Draws), UpdatedAt: timestamppb.New(r.UpdatedAt),
		}
	}
	return connect.NewResponse(out), nil
}

package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/admincrud"
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

var AdminRules = authz.Rules{
	gamev1connect.GameAdminServiceListGameSessionsProcedure:     authz.GamesRead,
	gamev1connect.GameAdminServiceCancelGameSessionProcedure:    authz.GamesModerate,
	gamev1connect.GameAdminServiceGetGameSessionProcedure:       authz.GamesRead,
	gamev1connect.GameAdminServiceListMatchQueueProcedure:       authz.GamesRead,
	gamev1connect.GameAdminServiceRemoveFromMatchQueueProcedure: authz.GamesModerate,
}

const (
	defaultAdminPage = 25
	maxAdminPage     = 100
)

type adminStore interface {
	AdminSession(ctx context.Context, id uuid.UUID, limit int) (session.Session, []session.MoveRecord, error)
	MatchQueue(ctx context.Context, f session.QueueFilter) (session.QueuePage, error)
	LeaveQueue(ctx context.Context, user uuid.UUID, game engine.Key) error
}

type Admin struct {
	h      *Handler
	store  adminStore
	record authz.Recorder
	log    *slog.Logger
}

var _ gamev1connect.GameAdminServiceHandler = (*Admin)(nil)

func NewAdmin(h *Handler, record authz.Recorder, log *slog.Logger) *Admin {
	return &Admin{h: h, store: h.store, record: record, log: log}
}

func (a *Admin) knownGame(game engine.Key) error {
	if _, err := a.h.store.Games().Definition(game); err != nil {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	}
	return nil
}

func optionalID(field, raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New(field+" is not valid"))
	}
	return &id, nil
}

func invalidQuery(err error) error {
	var invalid *adminquery.Error
	if errors.As(err, &invalid) {
		return admincrud.QueryError(err)
	}
	return nil
}

func (a *Admin) ListGameSessions(ctx context.Context, req *connect.Request[gamev1.ListGameSessionsRequest]) (*connect.Response[gamev1.ListGameSessionsResponse], error) {
	filter := session.AdminFilter{
		Game: engine.Key(req.Msg.GetGame()), Cursor: req.Msg.GetCursor(), Limit: int(req.Msg.GetPageSize()),
		List: adminquery.Request{Filter: req.Msg.GetFilter(), OrderBy: req.Msg.GetOrderBy(), Offset: req.Msg.Offset, PageSize: req.Msg.GetPageSize()},
	}
	if filter.Game != "" {
		if err := a.knownGame(filter.Game); err != nil {
			return nil, err
		}
	}
	switch req.Msg.GetStatus() {
	case gamev1.GameSessionFilter_GAME_SESSION_FILTER_LIVE:
		filter.State = session.AdminLive
	case gamev1.GameSessionFilter_GAME_SESSION_FILTER_WAITING:
		filter.State = session.AdminWaiting
	case gamev1.GameSessionFilter_GAME_SESSION_FILTER_FINISHED:
		filter.State = session.AdminFinished
	}
	var err error
	if filter.Player, err = optionalID("player_id", req.Msg.GetPlayerId()); err != nil {
		return nil, err
	}
	if filter.Tournament, err = optionalID("tournament_id", req.Msg.GetTournamentId()); err != nil {
		return nil, err
	}
	switch {
	case filter.Limit <= 0:
		filter.Limit = defaultAdminPage
	case filter.Limit > maxAdminPage:
		filter.Limit = maxAdminPage
	}

	page, err := a.h.store.Replica().AdminList(ctx, filter)
	if invalid := invalidQuery(err); invalid != nil {
		return nil, invalid
	}
	if errors.Is(err, session.ErrInvalidCursor) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cursor is not valid"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "could not list games for an administrator", "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	out := &gamev1.ListGameSessionsResponse{NextCursor: page.Next, Total: page.Total, Sessions: make([]*gamev1.Session, 0, len(page.Sessions))}
	for _, item := range page.Sessions {
		state, err := a.h.result(item, uuid.Nil, nil)
		if err != nil {
			return nil, err
		}
		out.Sessions = append(out.Sessions, state)
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) CancelGameSession(ctx context.Context, req *connect.Request[gamev1.CancelGameSessionRequest]) (*connect.Response[gamev1.CancelGameSessionResponse], error) {
	id, err := sessionID(req.Msg.GetSessionId())
	if err != nil {
		return nil, err
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	current, err := a.h.store.Get(ctx, id)
	if errors.Is(err, session.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("game not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load the game"))
	}
	if current.Tournament != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("a tournament game ends with its tournament"))
	}
	if current.Opponent != nil && current.State.Over() {
		return nil, gameOver()
	}

	cancelled, err := a.h.cancelGame(ctx, id)
	if err := ownerError(err); err != nil {
		return nil, err
	}
	if !cancelled {
		return nil, gameOver()
	}

	action := authz.NewAction(ctx, "game", "session_cancelled", "game_session", id.String(), reason)
	action.Details = map[string]string{"game": string(current.Game), "creator_id": current.Creator.String()}
	if current.Opponent != nil {
		action.Details["opponent_id"] = current.Opponent.String()
	}
	if current.Tournament != nil {
		action.Details["tournament_id"] = current.Tournament.String()
	}
	if err := a.record(context.WithoutCancel(ctx), action); err != nil {
		a.log.ErrorContext(ctx, "could not record a cancelled game", "session_id", id, "error", err)
	}
	a.log.InfoContext(ctx, "game cancelled by an administrator", "session_id", id, "actor_id", action.ActorID)
	return connect.NewResponse(&gamev1.CancelGameSessionResponse{}), nil
}

func (a *Admin) GetGameSession(ctx context.Context, req *connect.Request[gamev1.GetGameSessionRequest]) (*connect.Response[gamev1.GetGameSessionResponse], error) {
	id, err := sessionID(req.Msg.GetSessionId())
	if err != nil {
		return nil, err
	}
	item, moves, err := a.store.AdminSession(ctx, id, session.AdminMoveLimit)
	if errors.Is(err, session.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("game not found"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "could not load a game for an administrator", "session_id", id, "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load the game"))
	}
	state, err := a.h.result(item, uuid.Nil, nil)
	if err != nil {
		return nil, err
	}
	out := &gamev1.GetGameSessionResponse{Session: state, Moves: make([]*gamev1.AdminMove, len(moves))}
	for i, move := range moves {
		out.Moves[i] = &gamev1.AdminMove{
			Ply: int32(move.Ply), PlayerId: move.Player.String(), Data: string(move.Data), PlayedAt: timestamppb.New(move.PlayedAt),
		}
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) ListMatchQueue(ctx context.Context, req *connect.Request[gamev1.ListMatchQueueRequest]) (*connect.Response[gamev1.ListMatchQueueResponse], error) {
	game := engine.Key(req.Msg.GetGame())
	if game != "" {
		if err := a.knownGame(game); err != nil {
			return nil, err
		}
	}
	page, err := a.store.MatchQueue(ctx, session.QueueFilter{
		Game: game, Limit: session.AdminQueueLimit,
		List: adminquery.Request{Filter: req.Msg.GetFilter(), OrderBy: req.Msg.GetOrderBy(), Offset: req.Msg.Offset, PageSize: req.Msg.GetPageSize()},
	})
	if invalid := invalidQuery(err); invalid != nil {
		return nil, invalid
	}
	if err != nil {
		a.log.ErrorContext(ctx, "could not list the match queue for an administrator", "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load the queue"))
	}
	out := &gamev1.ListMatchQueueResponse{Total: page.Total, Entries: make([]*gamev1.QueueEntry, len(page.Entries))}
	for i, entry := range page.Entries {
		out.Entries[i] = &gamev1.QueueEntry{
			UserId: entry.User.String(), Game: string(entry.Game), Rating: int32(entry.Rating),
			JoinedAt: timestamppb.New(entry.JoinedAt), LastSeenAt: timestamppb.New(entry.LastSeenAt),
		}
		if entry.Session != nil {
			out.Entries[i].SessionId = entry.Session.String()
		}
	}
	return connect.NewResponse(out), nil
}

func (a *Admin) RemoveFromMatchQueue(ctx context.Context, req *connect.Request[gamev1.RemoveFromMatchQueueRequest]) (*connect.Response[gamev1.RemoveFromMatchQueueResponse], error) {
	user, err := uuid.Parse(req.Msg.GetUserId())
	if err != nil || user == uuid.Nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id is not valid"))
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	game := engine.Key(req.Msg.GetGame())
	if err := a.knownGame(game); err != nil {
		return nil, err
	}
	err = a.store.LeaveQueue(ctx, user, game)
	if errors.Is(err, session.ErrNotSearching) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("this player is not waiting in the queue"))
	}
	if err != nil {
		a.log.ErrorContext(ctx, "could not remove a player from the match queue", "user_id", user, "game", game, "error", err)
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not update the queue"))
	}

	action := authz.NewAction(ctx, "game", "queue_entry_removed", "user", user.String(), reason)
	action.Details = map[string]string{"game": string(game)}
	if err := a.record(context.WithoutCancel(ctx), action); err != nil {
		a.log.ErrorContext(ctx, "could not record a removed queue entry", "user_id", user, "game", game, "error", err)
	}
	a.log.InfoContext(ctx, "player removed from the match queue by an administrator", "user_id", user, "game", game, "actor_id", action.ActorID)
	return connect.NewResponse(&gamev1.RemoveFromMatchQueueResponse{}), nil
}

func gameOver() error {
	return connect.NewError(connect.CodeFailedPrecondition, errors.New("the game is already over"))
}

func aborted(game session.Session) bool {
	return game.ID != uuid.Nil && (game.Opponent == nil || !game.State.Over())
}

func (h *Handler) cancelGame(ctx context.Context, id uuid.UUID) (bool, error) {
	var cancelled bool
	err := h.onOwnerOf(id, func() error {
		game, err := h.owner.Abort(ctx, id)
		cancelled = aborted(game)
		return err
	}, func(peer gamev1connect.GameOwnerServiceClient) error {
		resp, err := peer.AbortSession(ctx, connect.NewRequest(&gamev1.AbortSessionRequest{SessionId: id.String()}))
		if err == nil {
			cancelled = resp.Msg.Cancelled == nil || resp.Msg.GetCancelled()
		}
		return err
	})
	return cancelled, err
}

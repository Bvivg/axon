package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"google.golang.org/protobuf/proto"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const Protocol = "axon.game.v1"

type Handler struct {
	gamev1connect.UnimplementedGameServiceHandler
	store    *session.Store
	verifier *authn.Verifier
	updates  updates
	matches  updates
}

func New(store *session.Store, verifier *authn.Verifier) *Handler {
	return &Handler{
		store: store, verifier: verifier,
		updates: updates{sessions: make(map[uuid.UUID]map[chan struct{}]struct{})},
		matches: updates{sessions: make(map[uuid.UUID]map[chan struct{}]struct{})},
	}
}

func (h *Handler) identity(headers http.Header) (uuid.UUID, authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return uuid.Nil, authn.Claims{}, err
	}
	claims, err := h.verifier.Verify(raw)
	if err != nil {
		return uuid.Nil, authn.Claims{}, err
	}
	return claims.UserID, claims, nil
}

func (h *Handler) actor(headers http.Header) (uuid.UUID, error) {
	user, _, err := h.identity(headers)
	if err != nil {
		return uuid.Nil, connect.NewError(connect.CodeUnauthenticated, errors.New("sign in to play"))
	}
	return user, nil
}

func (h *Handler) CreateSession(ctx context.Context, req *connect.Request[gamev1.CreateSessionRequest]) (*connect.Response[gamev1.CreateSessionResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	request, err := uuid.Parse(req.Msg.RequestId)
	if err != nil || request == uuid.Nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid request ID"))
	}
	item, err := h.store.Create(ctx, user, request, engine.Key(req.Msg.Game))
	state, err := h.view(ctx, item, err)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.CreateSessionResponse{Session: state}), nil
}

func (h *Handler) JoinSession(ctx context.Context, req *connect.Request[gamev1.JoinSessionRequest]) (*connect.Response[gamev1.JoinSessionResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := sessionID(req.Msg.SessionId)
	if err != nil {
		return nil, err
	}
	item, err := h.store.Join(ctx, id, user)
	state, err := h.view(ctx, item, err)
	if err != nil {
		return nil, err
	}
	h.updates.notify(id)
	return connect.NewResponse(&gamev1.JoinSessionResponse{Session: state}), nil
}

func (h *Handler) GetSession(ctx context.Context, req *connect.Request[gamev1.GetSessionRequest]) (*connect.Response[gamev1.GetSessionResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := sessionID(req.Msg.SessionId)
	if err != nil {
		return nil, err
	}
	item, err := h.store.Get(ctx, id, user)
	state, err := h.view(ctx, item, err)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.GetSessionResponse{Session: state}), nil
}

func (h *Handler) ListSessions(ctx context.Context, req *connect.Request[gamev1.ListSessionsRequest]) (*connect.Response[gamev1.ListSessionsResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	items, err := h.store.List(ctx, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	var rated []uuid.UUID
	for _, item := range items {
		if item.Rated && item.State.Over() {
			rated = append(rated, item.ID)
		}
	}
	changes, err := h.store.Changes(ctx, rated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	response := &gamev1.ListSessionsResponse{}
	for _, item := range items {
		state, err := h.result(item, nil)
		if err != nil {
			return nil, err
		}
		attach(state, changes[item.ID])
		response.Sessions = append(response.Sessions, state)
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) view(ctx context.Context, item session.Session, err error) (*gamev1.Session, error) {
	state, err := h.result(item, err)
	if err != nil || !item.Rated || !item.State.Over() {
		return state, err
	}
	changes, err := h.store.Changes(ctx, []uuid.UUID{item.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("game service unavailable"))
	}
	attach(state, changes[item.ID])
	return state, nil
}

func attach(state *gamev1.Session, changes []session.RatingChange) {
	for _, change := range changes {
		state.RatingChanges = append(state.RatingChanges, &gamev1.RatingChange{UserId: change.User.String(), Before: uint32(change.Before), After: uint32(change.After)})
	}
}

func sessionID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid game link"))
	}
	return id, nil
}

func (h *Handler) result(item session.Session, err error) (*gamev1.Session, error) {
	return present(item, h.store.TimeLimitMS(item.Game), err)
}

func present(item session.Session, limit int64, err error) (*gamev1.Session, error) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, session.ErrFull):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, engine.ErrUnknownGame):
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, errors.New("game service unavailable"))
	}
	out := &gamev1.Session{
		Id: item.ID.String(), Game: string(item.Game), PlayerIds: []string{item.Creator.String()},
		Status:    gamev1.SessionStatus_SESSION_STATUS_WAITING,
		UpdatedAt: timestamppb.New(item.UpdatedAt), Revision: proto.Uint32(item.Revision), TimedOut: proto.Bool(item.TimedOut),
		Rated: proto.Bool(item.Rated), Resigned: proto.Bool(item.Resigned), AgreedDraw: proto.Bool(item.AgreedDraw),
	}
	if item.DrawOffer != nil {
		offer := item.DrawOffer.String()
		out.DrawOfferUserId = &offer
	}
	switch item.Game {
	case tictactoe.Key:
		out.TicTacToe = &gamev1.TicTacToeBoard{Cells: make([]gamev1.Mark, 9)}
	case chess.Key:
		out.Chess = &gamev1.ChessBoard{Fen: chess.StartFEN}
	}
	if item.Opponent == nil {
		out.Clock = &gamev1.SessionClock{XRemainingMs: proto.Uint32(uint32(limit)), ORemainingMs: proto.Uint32(uint32(limit)), SampledAt: timestamppb.Now()}
		return out, nil
	}
	if clock := item.ClockAt(time.Now()); clock != nil {
		out.Clock = &gamev1.SessionClock{XRemainingMs: proto.Uint32(uint32(clock.RemainingMS[0])), ORemainingMs: proto.Uint32(uint32(clock.RemainingMS[1])), SampledAt: timestamppb.New(clock.StartedAt)}
	}
	out.PlayerIds = append(out.PlayerIds, item.Opponent.String())
	out.Ply = uint32(item.State.Ply)
	out.Status = gamev1.SessionStatus_SESSION_STATUS_PLAYING
	if err := board(out, item); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("invalid saved game"))
	}
	if user, ok := item.State.Current(); ok {
		id := string(user)
		out.TurnUserId = &id
	} else if _, winner := item.State.Outcome(); winner != nil {
		id := string(*winner)
		out.Status, out.WinnerUserId = gamev1.SessionStatus_SESSION_STATUS_WON, &id
	} else {
		out.Status = gamev1.SessionStatus_SESSION_STATUS_DRAW
	}
	return out, nil
}

func (h *Handler) LeaveSession(ctx context.Context, req *connect.Request[gamev1.LeaveSessionRequest]) (*connect.Response[gamev1.LeaveSessionResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := sessionID(req.Msg.SessionId)
	if err != nil {
		return nil, err
	}
	item, cancelled, err := h.store.Leave(ctx, id, user)
	if cancelled && err == nil {
		h.updates.notify(id)
		return connect.NewResponse(&gamev1.LeaveSessionResponse{Cancelled: true}), nil
	}
	state, err := h.view(ctx, item, err)
	if err != nil {
		return nil, err
	}
	h.updates.notify(id)
	return connect.NewResponse(&gamev1.LeaveSessionResponse{Session: state}), nil
}

var chessEnds = map[chess.End]gamev1.ChessEnd{
	chess.Checkmate:            gamev1.ChessEnd_CHESS_END_CHECKMATE,
	chess.Stalemate:            gamev1.ChessEnd_CHESS_END_STALEMATE,
	chess.InsufficientMaterial: gamev1.ChessEnd_CHESS_END_INSUFFICIENT_MATERIAL,
	chess.ThreefoldRepetition:  gamev1.ChessEnd_CHESS_END_THREEFOLD_REPETITION,
	chess.FiftyMoves:           gamev1.ChessEnd_CHESS_END_FIFTY_MOVES,
}

func board(out *gamev1.Session, item session.Session) error {
	switch item.Game {
	case tictactoe.Key:
		cells, winning, err := tictactoe.Position(item.State)
		if err != nil {
			return err
		}
		for i, seat := range cells {
			out.TicTacToe.Cells[i] = gamev1.Mark(seat + 1)
		}
		for _, cell := range winning {
			out.TicTacToe.WinningCells = append(out.TicTacToe.WinningCells, uint32(cell))
		}
	case chess.Key:
		snap, err := chess.View(item.State)
		if err != nil {
			return err
		}
		out.Chess = &gamev1.ChessBoard{Fen: snap.FEN, San: snap.SAN, Check: snap.Check, LegalMoves: snap.Legal, End: chessEnds[snap.End]}
		if snap.LastMove != "" {
			out.Chess.LastMove = &snap.LastMove
		}
	}
	return nil
}

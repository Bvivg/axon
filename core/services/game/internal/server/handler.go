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
	"github.com/bvivg/axon/core/services/game/internal/games/checkers"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/metrics"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/route"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const Protocol = "axon.game.v1"

type Config struct {
	Store        *session.Store
	Owner        *owner.Owner
	Routes       *route.Table
	Self         string
	Verifier     *authn.Verifier
	Client       *http.Client
	Sockets      *http.Client
	Secure       bool
	TestHooks    bool
	Metrics      *metrics.Game
	Watchers     int
	History      HistoryLimits
	MatchSockets int
	OwnerKey     string
}

type HistoryLimits struct {
	Plies   int
	Bytes   int
	Entries int
}

type Handler struct {
	gamev1connect.UnimplementedGameServiceHandler
	gamev1connect.UnimplementedGameOwnerServiceHandler
	store    *session.Store
	owner    *owner.Owner
	metrics  *metrics.Game
	routes   *route.Table
	self     string
	verifier *authn.Verifier
	hooks    bool
	peers    *peers
	sockets  *http.Client
	scheme   string
	updates  updates
	matches  updates
	watchers *watchers
	ownerKey string

	historyPlies int
	historyBytes int
	histories    *historyCache
	searches     *matchFeeds
}

func New(cfg Config) *Handler {
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	scheme := "http"
	if cfg.Secure {
		scheme = "https"
	}
	h := &Handler{
		store: cfg.Store, owner: cfg.Owner, routes: cfg.Routes, self: cfg.Self, verifier: cfg.Verifier, hooks: cfg.TestHooks, metrics: cfg.Metrics,
		peers:    newPeers(client, scheme, cfg.OwnerKey),
		sockets:  cfg.Sockets,
		scheme:   scheme,
		updates:  newUpdates(),
		matches:  newUpdates(),
		watchers: newWatchers(cfg.Watchers),
		ownerKey: cfg.OwnerKey,

		historyPlies: cfg.History.Plies,
		historyBytes: cfg.History.Bytes,
		histories:    newHistoryCache(cfg.History.Entries),
	}
	h.searches = newMatchFeeds(cfg.MatchSockets, matchPollInterval, h.pollMatch, h.watchMatches)
	return h
}

func (h *Handler) Changed(id uuid.UUID) { h.updates.notify(id) }

func (h *Handler) Moved(id uuid.UUID) { h.updates.move(id) }

func (h *Handler) identity(headers http.Header) (uuid.UUID, authn.Claims, error) {
	if h.hooks {
		if raw := headers.Get(TestUserHeader); raw != "" {
			user, err := uuid.Parse(raw)
			if err != nil || user == uuid.Nil {
				return uuid.Nil, authn.Claims{}, errors.New("invalid test user")
			}
			return user, authn.Claims{UserID: user, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
		}
	}
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
	if _, err := h.store.Games().Get(engine.Key(req.Msg.Game)); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	}
	id := session.SessionID(user, request)
	return onOwner(h, ctx, id, req, func() (*connect.Response[gamev1.CreateSessionResponse], error) {
		item, err := h.owner.Create(ctx, user, request, engine.Key(req.Msg.Game))
		state, err := h.respond(ctx, item, user, err)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&gamev1.CreateSessionResponse{Session: state}), nil
	}, gamev1connect.GameServiceClient.CreateSession)
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
	return onOwner(h, ctx, id, req, func() (*connect.Response[gamev1.JoinSessionResponse], error) {
		item, err := h.owner.Join(ctx, id, user)
		state, err := h.respond(ctx, item, user, err)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&gamev1.JoinSessionResponse{Session: state}), nil
	}, gamev1connect.GameServiceClient.JoinSession)
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
	return onOwner(h, ctx, id, req, func() (*connect.Response[gamev1.GetSessionResponse], error) {
		item, err := h.owner.Get(ctx, id, user)
		state, err := h.respond(ctx, item, user, err)
		if err != nil {
			return nil, err
		}
		state.Spectators = proto.Uint32(h.watchers.count(id))
		return connect.NewResponse(&gamev1.GetSessionResponse{Session: state}), nil
	}, gamev1connect.GameServiceClient.GetSession)
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
	return onOwner(h, ctx, id, req, func() (*connect.Response[gamev1.LeaveSessionResponse], error) {
		item, cancelled, err := h.owner.Leave(ctx, id, user)
		if cancelled && err == nil {
			return connect.NewResponse(&gamev1.LeaveSessionResponse{Cancelled: true}), nil
		}
		state, err := h.respond(ctx, item, user, err)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&gamev1.LeaveSessionResponse{Session: state}), nil
	}, gamev1connect.GameServiceClient.LeaveSession)
}

func (h *Handler) ListSessions(ctx context.Context, req *connect.Request[gamev1.ListSessionsRequest]) (*connect.Response[gamev1.ListSessionsResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	history := h.store.Replica()
	items, err := history.List(ctx, user)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	var rated []uuid.UUID
	for _, item := range items {
		if item.Rated && item.State.Over() {
			rated = append(rated, item.ID)
		}
	}
	changes, err := history.Changes(ctx, rated)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	response := &gamev1.ListSessionsResponse{}
	for _, item := range items {
		state, err := h.result(item, user, nil)
		if err != nil {
			return nil, err
		}
		attach(state, changes[item.ID])
		response.Sessions = append(response.Sessions, state)
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) view(ctx context.Context, item session.Session, viewer uuid.UUID, err error) (*gamev1.Session, error) {
	state, err := h.result(item, viewer, err)
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

func (h *Handler) result(item session.Session, viewer uuid.UUID, err error) (*gamev1.Session, error) {
	return present(visible(h.store.Games(), item, viewer), h.store.TimeLimitMS(item.Game), err)
}

func visible(games *engine.Registry, item session.Session, viewer uuid.UUID) session.Session {
	item.State = games.View(item.State, engine.PlayerID(viewer.String()))
	return item
}

func (h *Handler) respond(ctx context.Context, item session.Session, viewer uuid.UUID, err error) (*gamev1.Session, error) {
	if errors.Is(err, owner.ErrNotOwner) {
		return nil, errMoved
	}
	return h.view(ctx, item, viewer, err)
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
		MoveLimit: proto.Bool(item.State.MoveLimit),
	}
	if item.DrawOffer != nil {
		offer := item.DrawOffer.String()
		out.DrawOfferUserId = &offer
	}
	if item.Tournament != nil {
		out.TournamentId = proto.String(item.Tournament.String())
	}
	switch item.Game {
	case tictactoe.Key:
		out.TicTacToe = &gamev1.TicTacToeBoard{Cells: make([]gamev1.Mark, 9)}
	case chess.Key:
		out.Chess = &gamev1.ChessBoard{Fen: chess.StartFEN}
	case quoridor.Key:
		out.Quoridor = &gamev1.QuoridorBoard{Pawns: []string{"e1", "e9"}, WallsLeft: []uint32{quoridor.WallsEach, quoridor.WallsEach}}
	case checkers.Key:
		out.Checkers = &gamev1.CheckersBoard{Squares: checkersSquares(checkers.NewBoard().String())}
	case durak.Key:
		out.Durak = &gamev1.DurakBoard{}
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
	case quoridor.Key:
		snap, err := quoridor.View(item.State)
		if err != nil {
			return err
		}
		out.Quoridor = &gamev1.QuoridorBoard{Pawns: snap.Pawns, Walls: snap.Walls, WallsLeft: snap.Left, Moves: snap.Moves, LegalMoves: snap.Legal}
		if snap.LastMove != "" {
			out.Quoridor.LastMove = &snap.LastMove
		}
	case checkers.Key:
		snap, err := checkers.View(item.State)
		if err != nil {
			return err
		}
		out.Checkers = checkersBoard(snap)
	case durak.Key:
		snap, err := durak.Describe(item.State)
		if err != nil {
			return err
		}
		out.Durak = durakBoard(snap)
	}
	return nil
}

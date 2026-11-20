package server

import (
	"context"
	"errors"
	"sync"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const (
	defaultWatchers = 200
	liveLimit       = 30
)

type watchers struct {
	mu     sync.Mutex
	limit  int
	counts map[uuid.UUID]int
	sum    int
}

func newWatchers(limit int) *watchers {
	if limit <= 0 {
		limit = defaultWatchers
	}
	return &watchers{limit: limit, counts: map[uuid.UUID]int{}}
}

func (w *watchers) join(id uuid.UUID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.counts[id] >= w.limit {
		return false
	}
	w.counts[id]++
	w.sum++
	return true
}

func (w *watchers) leave(id uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.counts[id] == 0 {
		return
	}
	w.sum--
	if w.counts[id]--; w.counts[id] == 0 {
		delete(w.counts, id)
	}
}

func (w *watchers) count(id uuid.UUID) uint32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return uint32(w.counts[id])
}

func (w *watchers) total() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sum
}

func (h *Handler) GetSessionHistory(ctx context.Context, req *connect.Request[gamev1.GetSessionHistoryRequest]) (*connect.Response[gamev1.GetSessionHistoryResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := sessionID(req.Msg.SessionId)
	if err != nil {
		return nil, err
	}
	game, moves, err := h.store.Replica().History(ctx, id)
	if errors.Is(err, session.ErrNotFound) || err == nil && !game.Watchable(user) {
		return nil, connect.NewError(connect.CodeNotFound, session.ErrNotFound)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load the game history"))
	}
	states, err := session.NewRules(h.store.Games()).Frames(game, moves)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("invalid saved game"))
	}
	state, err := h.view(ctx, game, nil)
	if err != nil {
		return nil, err
	}
	response := &gamev1.GetSessionHistoryResponse{Session: state, Frames: make([]*gamev1.HistoryFrame, 0, len(states))}
	for i, position := range states {
		frame, err := historyFrame(game, position)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("invalid saved game"))
		}
		if i > 0 {
			player := moves[i-1].Player.String()
			frame.PlayerId, frame.PlayedAt = &player, timestamppb.New(moves[i-1].PlayedAt)
		}
		response.Frames = append(response.Frames, frame)
	}
	return connect.NewResponse(response), nil
}

func historyFrame(game session.Session, position engine.State) (*gamev1.HistoryFrame, error) {
	item := game
	item.State = position
	out := &gamev1.Session{}
	if game.Game == tictactoe.Key {
		out.TicTacToe = &gamev1.TicTacToeBoard{Cells: make([]gamev1.Mark, tictactoe.Size*tictactoe.Size)}
	}
	if err := board(out, item); err != nil {
		return nil, err
	}
	frame := &gamev1.HistoryFrame{Ply: uint32(position.Ply), TicTacToe: out.TicTacToe}
	switch game.Game {
	case chess.Key:
		frame.Chess = out.Chess
		frame.Chess.LegalMoves = nil
	case quoridor.Key:
		frame.Quoridor = out.Quoridor
		frame.Quoridor.LegalMoves = nil
	}
	return frame, nil
}

func (h *Handler) ListLiveSessions(ctx context.Context, req *connect.Request[gamev1.ListLiveSessionsRequest]) (*connect.Response[gamev1.ListLiveSessionsResponse], error) {
	if _, err := h.actor(req.Header()); err != nil {
		return nil, err
	}
	key := engine.Key(req.Msg.GetGame())
	if key != "" {
		if _, err := h.store.Games().Definition(key); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
		}
	}
	items, err := h.store.Replica().Live(ctx, key, liveLimit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load games"))
	}
	response := &gamev1.ListLiveSessionsResponse{Sessions: make([]*gamev1.Session, 0, len(items))}
	for _, item := range items {
		state, err := h.result(item, nil)
		if err != nil {
			return nil, err
		}
		response.Sessions = append(response.Sessions, state)
	}
	return connect.NewResponse(response), nil
}

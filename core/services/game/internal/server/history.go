package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const (
	DefaultHistoryPlies   = 1000
	DefaultHistoryBytes   = 1 << 20
	DefaultHistoryEntries = 32
)

var errHistoryTooLong = errors.New("game history is too long to replay")

type historyEntry struct {
	revision uint32
	frames   []*gamev1.HistoryFrame
	used     uint64
}

type historyCache struct {
	mu      sync.Mutex
	limit   int
	clock   uint64
	entries map[uuid.UUID]*historyEntry
}

func newHistoryCache(limit int) *historyCache {
	return &historyCache{limit: limit, entries: map[uuid.UUID]*historyEntry{}}
}

func (c *historyCache) revision(id uuid.UUID) (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok {
		return 0, false
	}
	return entry.revision, true
}

func (c *historyCache) get(id uuid.UUID, revision uint32) ([]*gamev1.HistoryFrame, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[id]
	if !ok || entry.revision != revision {
		return nil, false
	}
	c.clock++
	entry.used = c.clock
	return entry.frames, true
}

func (c *historyCache) put(id uuid.UUID, revision uint32, frames []*gamev1.HistoryFrame) {
	if c.limit <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	c.entries[id] = &historyEntry{revision: revision, frames: frames, used: c.clock}
	for len(c.entries) > c.limit {
		var oldest uuid.UUID
		least := c.clock + 1
		for key, entry := range c.entries {
			if entry.used < least {
				oldest, least = key, entry.used
			}
		}
		delete(c.entries, oldest)
	}
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
	game, frames, err := h.history(ctx, user, id)
	switch {
	case errors.Is(err, session.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, session.ErrNotFound)
	case errors.Is(err, errHistoryTooLong):
		return nil, connect.NewError(connect.CodeResourceExhausted, errHistoryTooLong)
	case errors.Is(err, context.DeadlineExceeded):
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	case errors.Is(err, context.Canceled):
		return nil, connect.NewError(connect.CodeCanceled, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load the game history"))
	}
	state, err := h.view(ctx, game, nil)
	if err != nil {
		return nil, err
	}
	response := &gamev1.GetSessionHistoryResponse{Session: state, Frames: frames}
	if size := proto.Size(response); h.historyBytes > 0 && size > h.historyBytes {
		return nil, connect.NewError(connect.CodeResourceExhausted, fmt.Errorf("game history of %d bytes exceeds the %d byte limit", size, h.historyBytes))
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) history(ctx context.Context, user, id uuid.UUID) (session.Session, []*gamev1.HistoryFrame, error) {
	replica := h.store.Replica()
	if _, cached := h.histories.revision(id); cached {
		game, err := replica.Get(ctx, id)
		if err == nil && game.Watchable(user) {
			if frames, ok := h.histories.get(id, game.Revision); ok {
				return game, frames, nil
			}
		}
	}
	game, moves, err := replica.History(ctx, id)
	if err != nil {
		return session.Session{}, nil, err
	}
	if !game.Watchable(user) {
		return session.Session{}, nil, session.ErrNotFound
	}
	frames, err := buildHistory(ctx, session.NewRules(h.store.Games()), game, moves, h.historyPlies)
	if err != nil {
		return session.Session{}, nil, err
	}
	if game.State.Over() {
		h.histories.put(id, game.Revision, frames)
	}
	return game, frames, nil
}

func buildHistory(ctx context.Context, rules session.Rules, game session.Session, moves []session.MoveRecord, maxPlies int) ([]*gamev1.HistoryFrame, error) {
	if maxPlies > 0 && len(moves) > maxPlies {
		return nil, fmt.Errorf("%w: %d plies, limit %d", errHistoryTooLong, len(moves), maxPlies)
	}
	frames := make([]*gamev1.HistoryFrame, 0, len(moves)+1)
	err := rules.Frames(ctx, game, moves, func(position engine.State) error {
		frame, err := historyFrame(game, position, len(frames) == len(moves))
		if err != nil {
			return err
		}
		if played := len(frames); played > 0 {
			player := moves[played-1].Player.String()
			frame.PlayerId, frame.PlayedAt = &player, timestamppb.New(moves[played-1].PlayedAt)
		}
		frames = append(frames, frame)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return frames, nil
}

func historyFrame(game session.Session, position engine.State, final bool) (*gamev1.HistoryFrame, error) {
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
		if n := len(frame.Chess.San); n > 0 {
			frame.Move = proto.String(frame.Chess.San[n-1])
		}
		if !final {
			frame.Chess.San = nil
		}
	case quoridor.Key:
		frame.Quoridor = out.Quoridor
		frame.Quoridor.LegalMoves = nil
		frame.Move = frame.Quoridor.LastMove
		if !final {
			frame.Quoridor.Moves = nil
		}
	}
	return frame, nil
}

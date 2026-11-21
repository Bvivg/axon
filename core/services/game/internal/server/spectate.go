package server

import (
	"context"
	"errors"
	"sync"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
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

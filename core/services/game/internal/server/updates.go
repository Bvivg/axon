package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type updates struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]map[chan struct{}]struct{}
}

func (u *updates) subscribe(id uuid.UUID) (<-chan struct{}, func()) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sessions[id] == nil {
		u.sessions[id] = make(map[chan struct{}]struct{})
	}
	inbox := make(chan struct{}, 1)
	u.sessions[id][inbox] = struct{}{}
	return inbox, func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		delete(u.sessions[id], inbox)
		if len(u.sessions[id]) == 0 {
			delete(u.sessions, id)
		}
	}
}

func (u *updates) notify(id uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.refresh(u.sessions[id])
}

func (u *updates) refreshAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, listeners := range u.sessions {
		u.refresh(listeners)
	}
}

func (*updates) refresh(listeners map[chan struct{}]struct{}) {
	for inbox := range listeners {
		select {
		case inbox <- struct{}{}:
		default:
		}
	}
}

func (h *Handler) Watch(ctx context.Context, log *slog.Logger) {
	for ctx.Err() == nil {
		err := h.store.Watch(ctx, h.updates.notify, h.matches.notify, func() {
			h.updates.refreshAll()
			h.matches.refreshAll()
		})
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "game session updates disconnected", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (h *Handler) RunClocks(ctx context.Context, log *slog.Logger) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := h.store.Expire(ctx); err != nil && ctx.Err() == nil {
			log.ErrorContext(ctx, "expire game clocks", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

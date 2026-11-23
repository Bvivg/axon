package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type listener struct {
	inbox chan struct{}
	gone  chan struct{}
	once  sync.Once
}

type updates struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]map[*listener]struct{}
}

func newUpdates() updates {
	return updates{sessions: map[uuid.UUID]map[*listener]struct{}{}}
}

func (u *updates) subscribe(id uuid.UUID) (<-chan struct{}, <-chan struct{}, func()) {
	item := &listener{inbox: make(chan struct{}, 1), gone: make(chan struct{})}
	return item.inbox, item.gone, u.add(id, item)
}

func (u *updates) share(id uuid.UUID, inbox chan struct{}) func() {
	return u.add(id, &listener{inbox: inbox, gone: make(chan struct{})})
}

func (u *updates) add(id uuid.UUID, item *listener) func() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.sessions[id] == nil {
		u.sessions[id] = map[*listener]struct{}{}
	}
	u.sessions[id][item] = struct{}{}
	return func() {
		u.mu.Lock()
		defer u.mu.Unlock()
		delete(u.sessions[id], item)
		if len(u.sessions[id]) == 0 {
			delete(u.sessions, id)
		}
	}
}

func (u *updates) notify(id uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	refresh(u.sessions[id])
}

func (u *updates) move(id uuid.UUID) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for item := range u.sessions[id] {
		item.once.Do(func() { close(item.gone) })
	}
}

func (u *updates) refreshAll() {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, listeners := range u.sessions {
		refresh(listeners)
	}
}

func refresh(listeners map[*listener]struct{}) {
	for item := range listeners {
		select {
		case item.inbox <- struct{}{}:
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

package server

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const feedLimit = 3

var errFeedSessions = errors.New("feed: invalid sessions")

type feedMark struct {
	revision   uint32
	spectators uint32
	ratings    int
	status     gamev1.SessionStatus
}

type feedSlot struct {
	release func()
	sent    *feedMark
}

type feed struct {
	h     *Handler
	inbox chan struct{}
	order []uuid.UUID
	slots map[uuid.UUID]*feedSlot
}

func feedSessions(watch *gamev1.WatchSessions) ([]uuid.UUID, error) {
	raw := watch.GetSessionIds()
	if len(raw) > feedLimit {
		return nil, errFeedSessions
	}
	ids := make([]uuid.UUID, 0, len(raw))
	for _, value := range raw {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil {
			return nil, errFeedSessions
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (h *Handler) watchFeed(ctx context.Context, first *gamev1.WatchSessions, write func(*gamev1.SessionEvent) error, incoming <-chan incoming) {
	fail := func(code gamev1.SessionError) error {
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Error{Error: code}})
	}
	f := &feed{h: h, inbox: make(chan struct{}, 1), slots: map[uuid.UUID]*feedSlot{}}
	defer f.close()
	ids, err := feedSessions(first)
	if err != nil {
		_ = fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
		return
	}
	f.watch(ids)
	if f.push(ctx, write) != nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.inbox:
			if f.push(ctx, write) != nil {
				return
			}
		case next := <-incoming:
			var code gamev1.SessionError
			switch {
			case next.limited:
				code = gamev1.SessionError_SESSION_ERROR_RATE_LIMITED
			case next.command == nil:
				code = gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND
			case next.command.GetWatch() == nil:
				code = gamev1.SessionError_SESSION_ERROR_SPECTATOR
			}
			if code == gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
				if ids, err = feedSessions(next.command.GetWatch()); err != nil {
					code = gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND
				}
			}
			if code != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
				if fail(code) != nil {
					return
				}
				continue
			}
			f.watch(ids)
			if f.push(ctx, write) != nil {
				return
			}
		}
	}
}

func (f *feed) watch(ids []uuid.UUID) {
	for id, slot := range f.slots {
		if !slices.Contains(ids, id) {
			slot.release()
			delete(f.slots, id)
		}
	}
	f.order = f.order[:0]
	for _, id := range ids {
		if f.slots[id] == nil {
			if !f.h.watchers.join(id) {
				continue
			}
			stop := f.h.updates.share(id, f.inbox)
			f.slots[id] = &feedSlot{release: func() {
				stop()
				f.h.watchers.leave(id)
				f.h.metrics.Spectators(f.h.watchers.total())
				f.h.updates.notify(id)
			}}
			f.h.metrics.Spectators(f.h.watchers.total())
			f.h.updates.notify(id)
		}
		f.order = append(f.order, id)
	}
}

func (f *feed) close() {
	for id, slot := range f.slots {
		slot.release()
		delete(f.slots, id)
	}
}

func (f *feed) push(ctx context.Context, write func(*gamev1.SessionEvent) error) error {
	for _, id := range f.order {
		slot := f.slots[id]
		item, err := f.h.peek(ctx, id)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			continue
		}
		state, err := f.h.view(ctx, item, uuid.Nil, nil)
		if err != nil {
			continue
		}
		state.Spectators = proto.Uint32(f.h.watchers.count(id))
		mark := feedMark{revision: state.GetRevision(), spectators: state.GetSpectators(), ratings: len(state.GetRatingChanges()), status: state.GetStatus()}
		if slot.sent != nil && *slot.sent == mark {
			continue
		}
		if err := write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Session{Session: state}}); err != nil {
			return err
		}
		slot.sent = &mark
	}
	return nil
}

func (h *Handler) peek(ctx context.Context, id uuid.UUID) (session.Session, error) {
	if h.owner.Serves(id) {
		item, err := h.owner.Get(ctx, id, uuid.Nil)
		if !errors.Is(err, owner.ErrNotOwner) {
			return item, err
		}
	}
	item, err := h.store.Get(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	if !item.Watchable(uuid.Nil) {
		return session.Session{}, session.ErrNotFound
	}
	return item, nil
}

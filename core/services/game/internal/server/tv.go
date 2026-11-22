package server

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/owner"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const (
	feedLimit     = 3
	feedRetry     = 250 * time.Millisecond
	feedRetryMost = 10 * time.Second
)

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
	remote  bool
	state   atomic.Pointer[gamev1.Session]
	lost    atomic.Bool
}

type feed struct {
	h      *Handler
	header http.Header
	inbox  chan struct{}
	order  []uuid.UUID
	slots  map[uuid.UUID]*feedSlot
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

func (h *Handler) watchFeed(ctx context.Context, header http.Header, first *gamev1.WatchSessions, write func(*gamev1.SessionEvent) error, incoming <-chan incoming) {
	fail := func(code gamev1.SessionError) error {
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Error{Error: code}})
	}
	f := &feed{h: h, header: header, inbox: make(chan struct{}, 1), slots: map[uuid.UUID]*feedSlot{}}
	defer f.close()
	ids, err := feedSessions(first)
	if err != nil {
		_ = fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
		return
	}
	f.watch(ctx, ids)
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
			f.watch(ctx, ids)
			if f.push(ctx, write) != nil {
				return
			}
		}
	}
}

func (f *feed) watch(ctx context.Context, ids []uuid.UUID) {
	for id, slot := range f.slots {
		if !slices.Contains(ids, id) {
			slot.release()
			delete(f.slots, id)
		}
	}
	f.order = f.order[:0]
	for _, id := range ids {
		if f.slots[id] == nil && f.attach(ctx, id) == nil {
			continue
		}
		f.order = append(f.order, id)
	}
}

func (f *feed) attach(ctx context.Context, id uuid.UUID) *feedSlot {
	slot := &feedSlot{}
	if f.h.owner.Serves(id) {
		if !f.h.watchers.join(id) {
			return nil
		}
		stop := f.h.updates.share(id, f.inbox)
		slot.release = func() {
			stop()
			f.h.watchers.leave(id)
			f.h.metrics.Spectators(f.h.watchers.total())
			f.h.updates.notify(id)
		}
		f.h.metrics.Spectators(f.h.watchers.total())
		f.h.updates.notify(id)
	} else {
		follow, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		slot.remote = true
		slot.release = func() {
			cancel()
			<-done
		}
		go func() {
			defer close(done)
			f.follow(follow, id, slot)
		}()
	}
	f.slots[id] = slot
	return slot
}

func (f *feed) close() {
	for id, slot := range f.slots {
		slot.release()
		delete(f.slots, id)
	}
}

func (f *feed) wake() {
	select {
	case f.inbox <- struct{}{}:
	default:
	}
}

func (f *feed) push(ctx context.Context, write func(*gamev1.SessionEvent) error) error {
	for _, id := range f.order {
		slot := f.slots[id]
		if slot != nil && (slot.lost.Load() || !slot.remote && !f.h.owner.Serves(id)) {
			slot.release()
			delete(f.slots, id)
			sent := slot.sent
			if slot = f.attach(ctx, id); slot != nil {
				slot.sent = sent
			}
		}
		if slot == nil {
			continue
		}
		state := f.current(ctx, id, slot)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if state == nil {
			continue
		}
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

func (f *feed) current(ctx context.Context, id uuid.UUID, slot *feedSlot) *gamev1.Session {
	if slot.remote {
		return slot.state.Load()
	}
	item, err := f.h.owner.Get(ctx, id, uuid.Nil)
	if errors.Is(err, owner.ErrNotOwner) {
		slot.lost.Store(true)
		time.AfterFunc(feedRetry, f.wake)
		return nil
	}
	if err != nil {
		return nil
	}
	state, err := f.h.view(ctx, item, uuid.Nil, nil)
	if err != nil {
		return nil
	}
	state.Spectators = proto.Uint32(f.h.watchers.count(id))
	return state
}

func (f *feed) follow(ctx context.Context, id uuid.UUID, slot *feedSlot) {
	var wait time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		routing, cancel := context.WithTimeout(ctx, routeTimeout)
		address, mine, err := f.h.ownerAddress(routing, id)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if mine {
			slot.lost.Store(true)
			f.wake()
			return
		}
		wait = min(max(2*wait, feedRetry), feedRetryMost)
		if err != nil {
			continue
		}
		delivered, refused := f.stream(ctx, address, id, slot)
		if refused {
			return
		}
		if delivered {
			wait = feedRetry
		}
	}
}

func (f *feed) stream(ctx context.Context, address string, id uuid.UUID, slot *feedSlot) (delivered, refused bool) {
	upstream, err := f.h.dialOwner(ctx, address, f.header)
	if err != nil {
		return false, false
	}
	defer func() { _ = upstream.CloseNow() }()
	first, err := protojson.Marshal(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: id.String()}})
	if err != nil || upstream.Write(ctx, first) != nil {
		return false, false
	}
	for {
		raw, err := upstream.Read(ctx)
		if err != nil {
			return delivered, false
		}
		var event gamev1.SessionEvent
		if protojson.Unmarshal(raw, &event) != nil {
			continue
		}
		if state := event.GetSession(); state != nil {
			slot.state.Store(state)
			delivered = true
			f.wake()
			continue
		}
		if !delivered {
			return false, true
		}
	}
}

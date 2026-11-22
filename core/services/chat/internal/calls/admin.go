package calls

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"
)

const Moderated = "moderated"

type AdminFilter struct {
	ActiveOnly bool
	User       *uuid.UUID
	Cursor     string
	Limit      int
}

type AdminPage struct {
	Calls       []Call
	Next        string
	ActiveTotal int64
}

type AdminStore interface {
	AdminCalls(ctx context.Context, f AdminFilter) (AdminPage, error)
}

func (s *Service) AdminCalls(ctx context.Context, f AdminFilter) (AdminPage, error) {
	store, ok := s.store.(AdminStore)
	if !ok {
		return AdminPage{}, ErrUnavailable
	}
	if f.Limit < 1 || f.Limit > 100 {
		f.Limit = 25
	}
	return store.AdminCalls(ctx, f)
}

func (s *Service) Terminate(ctx context.Context, id uuid.UUID, audit authz.Action) (Call, error) {
	now := s.now().UTC()
	cmd := Command{Action: "timeout", ID: id, Reason: Moderated, EnableSFU: s.SFUEnabled(), Audit: &audit}
	return s.store.Mutate(ctx, Actor{}, cmd, now, func(call *Call) (bool, error) {
		if call.State == Ended {
			return false, ErrConflict
		}
		audit.Details = map[string]string{
			"state":        call.State,
			"mode":         call.Mode,
			"kind":         kind(call),
			"participants": strconv.Itoa(len(call.Participants)),
			"duration_s":   strconv.FormatInt(int64(duration(call, now)/time.Second), 10),
		}
		if call.RoomID != uuid.Nil {
			audit.Details["room_id"] = call.RoomID.String()
		}
		call.finish(now, Moderated)
		if call.Clean() {
			call.Deadline = time.Time{}
		}
		call.Version++
		return true, nil
	})
}

func kind(call *Call) string {
	if call.Group() {
		return "group"
	}
	return "direct"
}

func duration(call *Call, now time.Time) time.Duration {
	if call.StartedAt == nil {
		return 0
	}
	return now.Sub(*call.StartedAt)
}

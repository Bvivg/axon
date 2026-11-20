package repository

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) persistCall(ctx context.Context, call calls.Call, now time.Time) error {
	users := make([]uuid.UUID, 0, len(call.Participants))
	for _, p := range call.Participants {
		users = append(users, p.UserID)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].String() < users[j].String() })
	if call.ReplacedByID == uuid.Nil {
		for _, user := range users {
			if _, err := r.q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "calls:slot:"+user.String()); err != nil {
				return err
			}
		}
	}
	document, err := json.Marshal(call)
	if err != nil {
		return err
	}
	if err := r.writeCall(ctx, call, document); err != nil {
		return err
	}
	for _, p := range call.Participants {
		var endpoint *uuid.UUID
		if p.EndpointID != uuid.Nil && (!call.Group() || !p.Retired() || !p.CleanupDone && !p.CleanupExpired) {
			endpoint = &p.EndpointID
		}
		if _, err := r.q.Exec(ctx, `INSERT INTO call_participants(call_id,user_id,endpoint_id) VALUES($1,$2,$3) ON CONFLICT(call_id,user_id) DO UPDATE SET endpoint_id=excluded.endpoint_id`, call.ID, p.UserID, endpoint); err != nil {
			return err
		}
		if call.ReplacedByID == uuid.Nil {
			released := (call.State == calls.Ended || p.Retired()) && (p.CleanupDone || p.CleanupExpired) && !p.SourcePending
			if released {
				if _, err := r.q.Exec(ctx, `DELETE FROM call_user_slots WHERE user_id=$1 AND call_id=$2`, p.UserID, call.ID); err != nil {
					return err
				}
			} else {
				phase := "busy"
				if call.State == calls.Ended || p.Retired() {
					phase = "releasing"
				}
				tag, err := r.q.Exec(ctx, `INSERT INTO call_user_slots(user_id,call_id,phase) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET phase=excluded.phase WHERE call_user_slots.call_id=excluded.call_id`, p.UserID, call.ID, phase)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return calls.ErrBusy
				}
			}
		}
		if call.TransportPhase == "waiting_cleanup" && call.PredecessorID != uuid.Nil && p.EndpointID == uuid.Nil {
			continue
		}
		if call.Group() && p.Retired() && p.CleanupDone && call.Version > p.RetiredVersion {
			continue
		}
		if _, err := r.q.Exec(ctx, `INSERT INTO call_outbox(event_id,call_id,user_id,version,body,created_at) VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), call.ID, p.UserID, call.Version, document, now); err != nil {
			return err
		}
	}
	if !r.sfu {
		return nil
	}
	state := call.State
	if call.TransportPhase == "waiting_cleanup" && state != calls.Ended {
		state = "waiting_cleanup"
	}
	var candidate int64
	if m := call.Migration; m != nil && call.State != calls.Ended && (m.Phase == "preparing" || m.Phase == "committing") {
		candidate = m.TargetGeneration
	}
	if _, err := r.q.Exec(ctx, `UPDATE call_transports SET state='ended',ended_at=COALESCE(ended_at,$3),setup_deadline=NULL,reconcile_at=$3 WHERE call_id=$1 AND generation<>$2 AND generation<>$4 AND state<>'ended'`, call.ID, call.Generation, now, candidate); err != nil {
		return err
	}
	var room *string
	if call.TransportKind() == "sfu" {
		name := "call_" + call.ID.String()
		room = &name
	}
	var deadline any
	if state == calls.Connecting || state == "waiting_cleanup" {
		deadline = nullCallTime(call.Deadline)
	}
	_, err = r.q.Exec(ctx, `INSERT INTO call_transports(call_id,generation,kind,state,room_name,setup_deadline,ended_at,reconcile_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(call_id,generation) DO UPDATE SET state=excluded.state,setup_deadline=excluded.setup_deadline,ended_at=excluded.ended_at,reconcile_at=excluded.reconcile_at`, call.ID, call.Generation, call.TransportKind(), state, room, deadline, call.EndedAt, now)
	if err != nil {
		return err
	}
	return r.persistMigration(ctx, call, now)
}
func (r *Repository) writeCall(ctx context.Context, call calls.Call, document []byte) error {
	const base = `INSERT INTO calls(id,room_id,caller_id,start_key,state,version,deadline,created_at,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET state=excluded.state,version=excluded.version,deadline=excluded.deadline,document=excluded.document`
	_, err := r.q.Exec(ctx, base, call.ID, nullCallID(call.RoomID), call.CallerID, call.StartKey, call.State, call.Version, nullCallTime(call.Deadline), call.CreatedAt, document)
	if err != nil || !r.sfu {
		return err
	}
	_, err = r.q.Exec(ctx, `UPDATE calls SET predecessor_call_id=$2,replaced_by_call_id=$3 WHERE id=$1`, call.ID, nullCallID(call.PredecessorID), nullCallID(call.ReplacedByID))
	return err
}

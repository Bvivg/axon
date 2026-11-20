package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) SetEndpointMigration(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, supported bool, now time.Time) error {
	if !r.migration {
		return calls.ErrUnavailable
	}
	return wrapCalls(r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.endpoint(ctx, actor, ref, now, true, true); err != nil {
			return err
		}
		_, err := tx.q.Exec(ctx, `UPDATE call_endpoints SET supports_migration=$2 AND supports_sfu WHERE id=$1`, ref.ID, supported)
		return err
	}))
}
func (r *Repository) persistMigration(ctx context.Context, call calls.Call, now time.Time) error {
	m := call.Migration
	if !r.migration || m == nil {
		return nil
	}
	document, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = r.q.Exec(ctx, `INSERT INTO call_migrations(id,call_id,source_generation,target_generation,phase,deadline,outcome,created_at,finished_at,document) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET phase=excluded.phase,deadline=excluded.deadline,outcome=excluded.outcome,finished_at=excluded.finished_at,document=excluded.document`, m.ID, call.ID, m.SourceGeneration, m.TargetGeneration, m.Phase, nullCallTime(m.Deadline), m.Outcome, m.CreatedAt, m.FinishedAt, document)
	if err != nil {
		return err
	}
	if call.TransportKind() == "sfu" && call.Generation == m.TargetGeneration {
		return nil
	}
	state := "ended"
	var ended any = now
	if call.State != calls.Ended && (m.Phase == "preparing" || m.Phase == "committing") {
		state = "connecting"
		ended = nil
	}
	_, err = r.q.Exec(ctx, `INSERT INTO call_transports(call_id,generation,kind,state,room_name,setup_deadline,ended_at,reconcile_at) VALUES($1,$2,'sfu',$3,$4,$5,$6,$7) ON CONFLICT(call_id,generation) DO UPDATE SET state=excluded.state,setup_deadline=excluded.setup_deadline,ended_at=COALESCE(call_transports.ended_at,excluded.ended_at),reconcile_at=excluded.reconcile_at`, call.ID, m.TargetGeneration, state, "call_"+call.ID.String(), nullCallTime(m.Deadline), ended, now)
	return err
}

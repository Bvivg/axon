package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func scanCallEvent(row scannable) (calls.Event, error) {
	var event calls.Event
	var raw []byte
	err := row.Scan(&event.ID, &event.UserID, &event.Sequence, &raw)
	if err != nil {
		return event, err
	}
	err = json.Unmarshal(raw, &event.Call)
	return event, err
}
func (r *Repository) Pending(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, now time.Time) ([]calls.Event, error) {
	if _, err := r.Endpoint(ctx, actor, ref, now, false); err != nil {
		return nil, err
	}
	rows, err := r.q.Query(ctx, `SELECT event_id,user_id,sequence,body FROM call_outbox WHERE user_id=$1 AND COALESCE((acknowledgements->>$2)::bigint,0)<$3 ORDER BY sequence LIMIT 100`, actor.UserID, ref.ID.String(), ref.Epoch)
	if err != nil {
		return nil, wrapCalls(err)
	}
	defer rows.Close()
	var result []calls.Event
	for rows.Next() {
		event, err := scanCallEvent(rows)
		if err != nil {
			return nil, wrapCalls(err)
		}
		result = append(result, event)
	}
	return result, wrapCalls(rows.Err())
}
func (r *Repository) Event(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, id uuid.UUID, now time.Time) (calls.Event, error) {
	if _, err := r.Endpoint(ctx, actor, ref, now, false); err != nil {
		return calls.Event{}, err
	}
	event, err := scanCallEvent(r.q.QueryRow(ctx, `SELECT event_id,user_id,sequence,body FROM call_outbox WHERE event_id=$1 AND user_id=$2 AND COALESCE((acknowledgements->>$3)::bigint,0)<$4`, id, actor.UserID, ref.ID.String(), ref.Epoch))
	return event, wrapCalls(err)
}
func (r *Repository) Acknowledge(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, id uuid.UUID, now time.Time) error {
	return wrapCalls(r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.endpoint(ctx, actor, ref, now, true, true); err != nil {
			return err
		}
		tag, err := tx.q.Exec(ctx, `UPDATE call_outbox SET acknowledgements=jsonb_set(acknowledgements,ARRAY[$3::text],to_jsonb($4::bigint)) WHERE event_id=$1 AND user_id=$2`, id, actor.UserID, ref.ID.String(), ref.Epoch)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return calls.ErrForbidden
		}
		return nil
	}))
}
func (r *Repository) DueEvents(ctx context.Context, now time.Time) ([]calls.Event, error) {
	rows, err := r.q.Query(ctx, `WITH due AS (SELECT o.sequence FROM call_outbox o JOIN calls c ON c.id=o.call_id WHERE o.next_attempt_at<=$1 AND (c.state<>'ended' OR o.created_at>$1-interval '1 minute') AND EXISTS(SELECT 1 FROM call_endpoints e WHERE e.user_id=o.user_id AND e.lease_expires_at>$1 AND COALESCE((o.acknowledgements->>e.id::text)::bigint,0)<e.epoch) ORDER BY o.next_attempt_at LIMIT 64 FOR UPDATE OF o SKIP LOCKED) UPDATE call_outbox o SET next_attempt_at=$1+interval '1 second',attempts=o.attempts+1 FROM due WHERE due.sequence=o.sequence RETURNING o.event_id,o.user_id,o.sequence,o.body`, now)
	if err != nil {
		return nil, wrapCalls(err)
	}
	defer rows.Close()
	var result []calls.Event
	for rows.Next() {
		event, err := scanCallEvent(rows)
		if err != nil {
			return nil, wrapCalls(err)
		}
		result = append(result, event)
	}
	return result, wrapCalls(rows.Err())
}
func (r *Repository) Expiring(ctx context.Context, now time.Time) ([]calls.Command, error) {
	rows, err := r.q.Query(ctx, `SELECT c.id,CASE WHEN c.deadline<=$1 THEN '' ELSE 'endpoint_expired' END FROM calls c WHERE c.deadline<=$1 OR (c.state<>'ended' AND EXISTS(SELECT 1 FROM call_participants p JOIN call_endpoints e ON e.id=p.endpoint_id WHERE p.call_id=c.id AND e.lease_expires_at<=$1)) ORDER BY c.id LIMIT 64`, now)
	if err != nil {
		return nil, wrapCalls(err)
	}
	defer rows.Close()
	var result []calls.Command
	for rows.Next() {
		cmd := calls.Command{Action: "timeout"}
		if err := rows.Scan(&cmd.ID, &cmd.Reason); err != nil {
			return nil, wrapCalls(err)
		}
		result = append(result, cmd)
	}
	return result, wrapCalls(rows.Err())
}

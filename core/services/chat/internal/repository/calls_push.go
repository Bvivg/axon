package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/notify"
)

func (r *Repository) DueCallPush(ctx context.Context, now time.Time) ([]notify.CallPushJob, error) {
	eligibility := `o.version=1 AND c.state='ringing' AND c.deadline>$1 AND o.user_id<>c.caller_id`
	if r.sfu {
		eligibility = `((COALESCE(c.document->>'kind','')<>'group' AND ` + eligibility + `) OR (
		c.document->>'kind'='group' AND c.state<>'ended' AND COALESCE(c.document->>'transport_phase','')<>'waiting_cleanup'
		AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.document->'participants') p, jsonb_array_elements(o.body->'participants') previous
		WHERE p->>'user_id'=o.user_id::text AND previous->>'user_id'=o.user_id::text
		AND p->>'status'='invited' AND previous->>'status'='invited'
		AND (p->>'invite_until')::timestamptz>$1 AND p->>'invite_until'=previous->>'invite_until'
		AND NOT EXISTS(SELECT 1 FROM call_outbox earlier, jsonb_array_elements(earlier.body->'participants') ep
		WHERE earlier.call_id=o.call_id AND earlier.user_id=o.user_id AND earlier.sequence<o.sequence
		AND ep->>'user_id'=o.user_id::text AND ep->>'status'='invited' AND ep->>'invite_until'=p->>'invite_until'))))`
	}
	rows, err := r.q.Query(ctx, `WITH due AS (
		SELECT o.sequence FROM call_outbox o JOIN calls c ON c.id=o.call_id
		WHERE NOT o.push_completed AND o.push_next_attempt_at<=$1 AND (`+eligibility+`)
		AND NOT EXISTS(SELECT 1 FROM call_endpoints e WHERE e.user_id=o.user_id AND e.lease_expires_at>$1 AND COALESCE((o.acknowledgements->>e.id::text)::bigint,0)>=e.epoch)
		ORDER BY o.push_next_attempt_at LIMIT 1 FOR UPDATE OF o SKIP LOCKED
	) UPDATE call_outbox o SET push_next_attempt_at=$1+interval '3 seconds',push_attempts=o.push_attempts+1
	FROM due,calls c WHERE due.sequence=o.sequence AND c.id=o.call_id
	RETURNING o.event_id,o.user_id,o.sequence,o.body,c.deadline,o.push_receipts`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []notify.CallPushJob
	for rows.Next() {
		var job notify.CallPushJob
		var raw, receipts []byte
		if err := rows.Scan(&job.Event.ID, &job.Event.UserID, &job.Event.Sequence, &raw, &job.Deadline, &receipts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &job.Event.Call); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(receipts, &job.Receipts); err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (r *Repository) RecordCallPush(ctx context.Context, event uuid.UUID, key string, complete bool) error {
	_, err := r.q.Exec(ctx, `UPDATE call_outbox SET push_completed=push_completed OR $3,push_receipts=CASE WHEN $2='' THEN push_receipts ELSE jsonb_set(push_receipts,ARRAY[$2::text],'true'::jsonb) END WHERE event_id=$1`, event, key, complete)
	return err
}

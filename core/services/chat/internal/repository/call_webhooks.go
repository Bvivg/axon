package repository

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) RecordWebhook(ctx context.Context, id string, hash []byte, room, kind string) error {
	if !r.sfu || len(id) == 0 || len(id) > 128 || len(hash) != 32 {
		return calls.ErrInvalid
	}
	_, err := r.q.Exec(ctx, `INSERT INTO call_webhook_receipts(event_id,body_hash,room_name,event_kind) VALUES($1,$2,$3,$4) ON CONFLICT(event_id) DO NOTHING`, id, hash, room, kind)
	if err != nil {
		return err
	}
	var previous []byte
	if err := r.q.QueryRow(ctx, `SELECT body_hash FROM call_webhook_receipts WHERE event_id=$1`, id).Scan(&previous); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(previous, hash) != 1 {
		return calls.ErrConflict
	}
	return nil
}
func (r *Repository) MediaRoomEnded(ctx context.Context, room string) (bool, error) {
	id, err := uuid.Parse(strings.TrimPrefix(room, "call_"))
	if err != nil {
		return false, calls.ErrInvalid
	}
	var raw []byte
	err = r.q.QueryRow(ctx, `SELECT document FROM calls WHERE id=$1`, id).Scan(&raw)
	if noRows(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var call calls.Call
	if err = json.Unmarshal(raw, &call); err != nil {
		return false, err
	}
	if call.State == calls.Ended {
		return true, nil
	}
	if call.TransportKind() == "sfu" {
		return false, nil
	}
	if r.migration && call.Migration != nil && (call.Migration.Phase == "preparing" || call.Migration.Phase == "committing") {
		return false, nil
	}
	return true, nil
}
func (r *Repository) ObserveMediaRoom(ctx context.Context, room, sid string) error {
	if !r.sfu {
		return calls.ErrUnavailable
	}
	_, err := r.q.Exec(ctx, `UPDATE call_transports SET room_sid=$2,reconcile_at=$3 WHERE room_name=$1 AND kind='sfu' AND state<>'ended'`, room, sid, time.Now().UTC())
	return err
}

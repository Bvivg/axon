package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) BindEndpoint(ctx context.Context, actor calls.Actor, ref calls.EndpointRef, connection uuid.UUID, now time.Time) error {
	if connection == uuid.Nil {
		return calls.ErrInvalid
	}
	return wrapCalls(r.InTx(ctx, func(tx *Repository) error {
		if _, err := tx.endpoint(ctx, actor, ref, now, true, false); err != nil {
			return err
		}
		tag, err := tx.q.Exec(ctx, `UPDATE call_endpoints SET connection_id=$2 WHERE id=$1 AND epoch=$3 AND lease_expires_at>$4 AND (connection_id IS NULL OR connection_id=$2)`, ref.ID, connection, ref.Epoch, now)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return calls.ErrStale
		}
		return nil
	}))
}

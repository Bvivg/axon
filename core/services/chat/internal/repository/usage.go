package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (r *Repository) ChargeUpload(ctx context.Context, user uuid.UUID, day time.Time, size, limit int64) (bool, error) {
	const query = `
		INSERT INTO upload_usage (user_id, day, bytes)
		SELECT $1::uuid, $2::date, $3::bigint WHERE $3::bigint <= $4::bigint
		ON CONFLICT (user_id) DO UPDATE
		SET bytes = CASE WHEN upload_usage.day < EXCLUDED.day THEN EXCLUDED.bytes
		                 ELSE upload_usage.bytes + EXCLUDED.bytes END,
		    day = GREATEST(upload_usage.day, EXCLUDED.day)
		WHERE upload_usage.day < EXCLUDED.day OR upload_usage.bytes + EXCLUDED.bytes <= $4::bigint`

	tag, err := r.q.Exec(ctx, query, user, day, size, limit)
	if err != nil {
		return false, fmt.Errorf("repository: charge upload budget: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) RefundUpload(ctx context.Context, user uuid.UUID, day time.Time, size int64) error {
	const query = `UPDATE upload_usage SET bytes = GREATEST(bytes - $3, 0) WHERE user_id = $1 AND day = $2::date`

	if _, err := r.q.Exec(ctx, query, user, day, size); err != nil {
		return fmt.Errorf("repository: refund upload budget: %w", err)
	}
	return nil
}

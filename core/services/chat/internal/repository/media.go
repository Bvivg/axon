package repository

import (
	"context"

	"github.com/google/uuid"
)

func (r *Repository) CanReadObject(ctx context.Context, user uuid.UUID, key string) (bool, error) {
	const query = `SELECT EXISTS (
		SELECT 1 FROM uploads u LEFT JOIN files f ON f.id = u.file_id
		WHERE (f.object_keys @> ARRAY[$2]::text[] OR (u.file_id IS NULL AND (
			right(u.payload->>'url', length($2)+1) = '/' || $2
			OR right(u.payload->>'thumbnail_url', length($2)+1) = '/' || $2
			OR right(u.payload->>'poster_url', length($2)+1) = '/' || $2)))
		AND ((u.uploader_id = $1
			AND NOT EXISTS (SELECT 1 FROM messages WHERE upload_id = u.id)
			AND NOT EXISTS (SELECT 1 FROM message_uploads WHERE upload_id = u.id)) OR EXISTS (
			SELECT 1 FROM messages m JOIN room_members rm ON rm.room_id = m.room_id AND rm.user_id = $1
			WHERE m.deleted_at IS NULL AND m.seq > rm.cleared_through_seq
			AND (m.upload_id = u.id OR EXISTS (
				SELECT 1 FROM message_uploads mu WHERE mu.message_id = m.id AND mu.upload_id = u.id))
		))
	)`
	var allowed bool
	err := r.q.QueryRow(ctx, query, user, key).Scan(&allowed)
	return allowed, err
}

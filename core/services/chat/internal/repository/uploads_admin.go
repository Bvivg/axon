package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const adminUploadsQuery = `
	SELECT u.id, u.uploader_id, u.kind,
		coalesce(u.payload->>'filename', f.payload->>'filename', ''),
		coalesce(u.payload->>'mime', f.payload->>'mime', ''),
		coalesce(f.size_bytes, CASE WHEN jsonb_typeof(u.payload->'size_bytes') = 'number' THEN (u.payload->>'size_bytes')::numeric::bigint END, 0),
		u.created_at, ref.room_id, ref.message_id
	FROM uploads u
	LEFT JOIN files f ON f.id = u.file_id
	LEFT JOIN LATERAL (
		SELECT linked.room_id, linked.id AS message_id
		FROM (
			SELECT m.id, m.room_id, m.sent_at FROM messages m WHERE m.upload_id = u.id
			UNION ALL
			SELECT m.id, m.room_id, m.sent_at FROM message_uploads mu JOIN messages m ON m.id = mu.message_id WHERE mu.upload_id = u.id
		) linked
		WHERE $2::uuid IS NULL OR linked.room_id = $2::uuid
		ORDER BY linked.sent_at, linked.id
		LIMIT 1
	) ref ON true
	WHERE ($1::uuid IS NULL OR u.uploader_id = $1::uuid)
		AND ($2::uuid IS NULL OR ref.room_id IS NOT NULL)
		AND ($3::text = '' OR u.kind = $3::text)
		AND ($4::timestamptz IS NULL OR (u.created_at, u.id) < ($4::timestamptz, $5::uuid))
	ORDER BY u.created_at DESC, u.id DESC
	LIMIT $6`

func (r *Repository) AdminUploads(ctx context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, error) {
	var uploader, room, at, id any
	if f.Uploader != nil {
		uploader = *f.Uploader
	}
	if f.Room != nil {
		room = *f.Room
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c roomCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", &domain.ValidationError{Field: "cursor", Reason: "is not valid"}
		}
		at, id = c.At, c.ID
	}
	rows, err := r.q.Query(ctx, adminUploadsQuery, uploader, room, string(f.Kind), at, id, f.Limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("repository: admin uploads: %w", err)
	}
	defer rows.Close()

	uploads := []domain.AdminUpload{}
	for rows.Next() {
		var (
			u    domain.AdminUpload
			kind string
		)
		if err := rows.Scan(&u.ID, &u.UploaderID, &kind, &u.Name, &u.ContentType, &u.SizeBytes, &u.CreatedAt, &u.RoomID, &u.MessageID); err != nil {
			return nil, "", fmt.Errorf("repository: scan admin upload: %w", err)
		}
		u.Kind = domain.MessageKind(kind)
		uploads = append(uploads, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("repository: admin uploads: %w", err)
	}
	next := ""
	if len(uploads) > f.Limit {
		uploads = uploads[:f.Limit]
		last := uploads[f.Limit-1]
		raw, _ := json.Marshal(roomCursor{At: last.CreatedAt, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return uploads, next, nil
}

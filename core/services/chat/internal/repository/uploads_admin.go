package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const (
	uploadName  = `coalesce(u.payload->>'filename', f.payload->>'filename', '')`
	uploadSize  = `coalesce(f.size_bytes, CASE WHEN jsonb_typeof(u.payload->'size_bytes') = 'number' THEN (u.payload->>'size_bytes')::numeric::bigint END, 0)`
	uploadLinks = `(
			SELECT m.id, m.room_id, m.sent_at FROM messages m WHERE m.upload_id = u.id
			UNION ALL
			SELECT m.id, m.room_id, m.sent_at FROM message_uploads mu JOIN messages m ON m.id = mu.message_id WHERE mu.upload_id = u.id
		) linked`
)

const adminUploadSelect = `
	SELECT u.id, u.uploader_id, u.kind, ` + uploadName + `,
		coalesce(u.payload->>'mime', f.payload->>'mime', ''),
		` + uploadSize + `,
		u.created_at, ref.room_id, ref.message_id`

var uploadSchema = adminquery.NewSchema(`u.created_at DESC, u.id DESC`, `u.id DESC`,
	adminquery.Field{Name: "id", Column: "u.id", Kind: adminquery.UUID},
	adminquery.Field{Name: "kind", Column: "u.kind", Kind: adminquery.Enum, Values: equalsOneOf("u.kind",
		string(domain.MessageKindVoice), string(domain.MessageKindAttachment), string(domain.MessageKindImage), string(domain.MessageKindVideo))},
	adminquery.Field{Name: "name", Column: uploadName, Kind: adminquery.String, Sortable: true},
	adminquery.Field{Name: "size", Column: uploadSize, Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "uploaded", Column: "u.created_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "uploader", Column: "u.uploader_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "room", Kind: adminquery.Custom,
		Build: matchesID("room", `EXISTS (SELECT 1 FROM `+uploadLinks+` WHERE linked.room_id = ?)`)},
)

func adminUploadsFrom(scope string) string {
	return `
	FROM uploads u
	LEFT JOIN files f ON f.id = u.file_id
	LEFT JOIN LATERAL (
		SELECT linked.room_id, linked.id AS message_id
		FROM ` + uploadLinks + scope + `
		ORDER BY linked.sent_at, linked.id
		LIMIT 1
	) ref ON true`
}

func (r *Repository) AdminUploads(ctx context.Context, f domain.AdminUploadFilter) ([]domain.AdminUpload, string, *int64, error) {
	var (
		where adminquery.Where
		scope string
	)
	if f.Room != nil {
		scope = ` WHERE linked.room_id = ` + where.Arg(*f.Room)
		where.Add("ref.room_id IS NOT NULL")
	}
	if f.Uploader != nil {
		where.Add("u.uploader_id = ?", *f.Uploader)
	}
	if f.Kind != "" {
		where.Add("u.kind = ?", string(f.Kind))
	}
	listed, err := uploadSchema.Prepare(f.List, &where)
	if err != nil {
		return nil, "", nil, fmt.Errorf("repository: admin uploads: %w", err)
	}
	from := adminUploadsFrom(scope)

	if f.List.Paged() {
		var total int64
		if err := r.q.QueryRow(ctx, `SELECT count(*)`+from+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return nil, "", nil, fmt.Errorf("repository: count admin uploads: %w", err)
		}
		limit, offset := f.List.Page()
		paging := where.Paginate(limit, offset)
		uploads, err := r.adminUploads(ctx, adminUploadSelect+from+where.SQL()+` ORDER BY `+listed.Order+paging, where.Args())
		if err != nil {
			return nil, "", nil, err
		}
		return uploads, "", &total, nil
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c roomCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", nil, &domain.ValidationError{Field: "cursor", Reason: "is not valid"}
		}
		where.Add("(u.created_at, u.id) < (?, ?)", c.At, c.ID)
	}
	limit := where.Arg(f.Limit + 1)
	uploads, err := r.adminUploads(ctx, adminUploadSelect+from+where.SQL()+` ORDER BY u.created_at DESC, u.id DESC LIMIT `+limit, where.Args())
	if err != nil {
		return nil, "", nil, err
	}
	next := ""
	if len(uploads) > f.Limit {
		uploads = uploads[:f.Limit]
		last := uploads[f.Limit-1]
		raw, _ := json.Marshal(roomCursor{At: last.CreatedAt, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return uploads, next, nil, nil
}

func (r *Repository) adminUploads(ctx context.Context, query string, args []any) ([]domain.AdminUpload, error) {
	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: admin uploads: %w", err)
	}
	defer rows.Close()

	uploads := []domain.AdminUpload{}
	for rows.Next() {
		var (
			u    domain.AdminUpload
			kind string
		)
		if err := rows.Scan(&u.ID, &u.UploaderID, &kind, &u.Name, &u.ContentType, &u.SizeBytes, &u.CreatedAt, &u.RoomID, &u.MessageID); err != nil {
			return nil, fmt.Errorf("repository: scan admin upload: %w", err)
		}
		u.Kind = domain.MessageKind(kind)
		uploads = append(uploads, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: admin uploads: %w", err)
	}
	return uploads, nil
}

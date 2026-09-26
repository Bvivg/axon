package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func (r *Repository) CreateUpload(ctx context.Context, u domain.Upload) (domain.Upload, error) {
	const query = `
		INSERT INTO uploads (id, uploader_id, kind, payload)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id, uploader_id, kind, payload, created_at`

	stored, err := scanUpload(r.q.QueryRow(ctx, query, u.ID, u.UploaderID, string(u.Kind), []byte(u.Payload)))
	if err != nil {
		return domain.Upload{}, fmt.Errorf("repository: create upload: %w", err)
	}
	return stored, nil
}

func (r *Repository) UploadByID(ctx context.Context, id uuid.UUID) (domain.Upload, error) {
	const query = `SELECT id, uploader_id, kind, payload, created_at FROM uploads WHERE id = $1`

	u, err := scanUpload(r.q.QueryRow(ctx, query, id))
	if err != nil {
		if noRows(err) {
			return domain.Upload{}, domain.ErrUploadNotFound
		}
		return domain.Upload{}, fmt.Errorf("repository: upload by id: %w", err)
	}
	return u, nil
}

func scanUpload(row scannable) (domain.Upload, error) {
	var (
		u       domain.Upload
		kind    string
		payload []byte
	)
	if err := row.Scan(&u.ID, &u.UploaderID, &kind, &payload, &u.CreatedAt); err != nil {
		return domain.Upload{}, err
	}
	u.Kind = domain.MessageKind(kind)
	u.Payload = json.RawMessage(payload)
	return u, nil
}

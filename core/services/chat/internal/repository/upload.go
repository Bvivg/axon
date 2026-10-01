package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

const unreferenced = `NOT EXISTS (SELECT 1 FROM messages m WHERE m.upload_id = u.id AND m.deleted_at IS NULL)`

func (r *Repository) SweepOrphanUploads(
	ctx context.Context,
	olderThan time.Time,
	limit int,
	remove func(context.Context, []domain.Upload) ([]uuid.UUID, error),
) (int, error) {
	var swept int

	err := r.InTx(ctx, func(tx *Repository) error {
		const pick = `
			SELECT u.id, u.uploader_id, u.kind, u.payload, u.created_at
			FROM uploads u
			WHERE u.created_at < $1 AND ` + unreferenced + `
			ORDER BY u.created_at
			LIMIT $2
			FOR UPDATE OF u SKIP LOCKED`

		rows, err := tx.q.Query(ctx, pick, olderThan, limit)
		if err != nil {
			return fmt.Errorf("repository: pick orphan uploads: %w", err)
		}

		var orphans []domain.Upload
		for rows.Next() {
			u, err := scanUpload(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("repository: scan orphan upload: %w", err)
			}
			orphans = append(orphans, u)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("repository: pick orphan uploads: %w", err)
		}
		orphans, err = tx.stillOrphaned(ctx, orphans)
		if err != nil {
			return err
		}
		if len(orphans) == 0 {
			return nil
		}

		ids, err := remove(ctx, orphans)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		if _, err := tx.q.Exec(ctx, `DELETE FROM uploads WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("repository: delete orphan uploads: %w", err)
		}

		swept = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}

	return swept, nil
}

func (r *Repository) stillOrphaned(ctx context.Context, picked []domain.Upload) ([]domain.Upload, error) {
	if len(picked) == 0 {
		return nil, nil
	}

	ids := make([]uuid.UUID, len(picked))
	for i, u := range picked {
		ids[i] = u.ID
	}

	const query = `SELECT u.id FROM uploads u WHERE u.id = ANY($1) AND ` + unreferenced

	rows, err := r.q.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("repository: recheck orphan uploads: %w", err)
	}
	defer rows.Close()

	orphaned := make(map[uuid.UUID]struct{}, len(picked))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("repository: scan orphan upload: %w", err)
		}
		orphaned[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: recheck orphan uploads: %w", err)
	}

	still := picked[:0]
	for _, u := range picked {
		if _, ok := orphaned[u.ID]; ok {
			still = append(still, u)
		}
	}
	return still, nil
}

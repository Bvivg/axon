package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const sessionColumns = `id, uploader_id, mode, filename, size_bytes, part_size, part_count,
	staging_key, multipart_id, status, failure, upload_id, created_at, updated_at`

func scanSession(row scannable) (domain.UploadSession, error) {
	var (
		s      domain.UploadSession
		mode   string
		status string
	)
	if err := row.Scan(
		&s.ID, &s.UploaderID, &mode, &s.Filename, &s.SizeBytes, &s.PartSize, &s.PartCount,
		&s.StagingKey, &s.MultipartID, &status, &s.Failure, &s.UploadID, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return domain.UploadSession{}, err
	}
	s.Mode = domain.UploadMode(mode)
	s.Status = domain.UploadStatus(status)
	return s, nil
}

func (r *Repository) CreateSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	const query = `
		INSERT INTO upload_sessions (id, uploader_id, mode, filename, size_bytes, part_size, part_count, staging_key, multipart_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING ` + sessionColumns

	stored, err := scanSession(r.q.QueryRow(ctx, query,
		s.ID, s.UploaderID, string(s.Mode), s.Filename, s.SizeBytes, s.PartSize, s.PartCount, s.StagingKey, s.MultipartID))
	if err != nil {
		return domain.UploadSession{}, fmt.Errorf("repository: create upload session: %w", err)
	}
	return stored, nil
}

func (r *Repository) SessionByID(ctx context.Context, id uuid.UUID) (domain.UploadSession, error) {
	s, err := scanSession(r.q.QueryRow(ctx, `SELECT `+sessionColumns+` FROM upload_sessions WHERE id = $1`, id))
	if err != nil {
		if noRows(err) {
			return domain.UploadSession{}, domain.ErrSessionNotFound
		}
		return domain.UploadSession{}, fmt.Errorf("repository: upload session: %w", err)
	}
	return s, nil
}

func (r *Repository) MarkSessionProcessing(ctx context.Context, id uuid.UUID) error {
	const query = `
		UPDATE upload_sessions
		SET status = 'processing', claimed_at = NULL, updated_at = now()
		WHERE id = $1 AND status = 'uploading'`

	tag, err := r.q.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("repository: mark upload processing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSessionState
	}
	return nil
}

func (r *Repository) ClaimSession(ctx context.Context, id uuid.UUID, staleAfter time.Duration) (domain.UploadSession, bool, error) {
	const query = `
		UPDATE upload_sessions
		SET claimed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'processing'
		  AND (claimed_at IS NULL OR claimed_at < now() - make_interval(secs => $2))
		RETURNING ` + sessionColumns

	s, err := scanSession(r.q.QueryRow(ctx, query, id, staleAfter.Seconds()))
	if err != nil {
		if noRows(err) {
			return domain.UploadSession{}, false, nil
		}
		return domain.UploadSession{}, false, fmt.Errorf("repository: claim upload session: %w", err)
	}
	return s, true, nil
}

func (r *Repository) ClaimableSessions(ctx context.Context, staleAfter time.Duration, limit int) ([]uuid.UUID, error) {
	const query = `
		SELECT id FROM upload_sessions
		WHERE status = 'processing' AND (claimed_at IS NULL OR claimed_at < now() - make_interval(secs => $1))
		ORDER BY updated_at
		LIMIT $2`

	rows, err := r.q.Query(ctx, query, staleAfter.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("repository: claimable upload sessions: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("repository: scan claimable upload session: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: claimable upload sessions: %w", err)
	}
	return ids, nil
}

func (r *Repository) FinishSession(ctx context.Context, id, uploadID uuid.UUID) error {
	const query = `
		UPDATE upload_sessions
		SET status = 'ready', upload_id = $2, claimed_at = NULL, failure = '', updated_at = now()
		WHERE id = $1 AND status = 'processing'`

	tag, err := r.q.Exec(ctx, query, id, uploadID)
	if err != nil {
		return fmt.Errorf("repository: finish upload session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrSessionState
	}
	return nil
}

func (r *Repository) FailSession(ctx context.Context, id uuid.UUID, reason string) error {
	const query = `
		UPDATE upload_sessions
		SET status = 'failed', failure = $2, claimed_at = NULL, updated_at = now()
		WHERE id = $1 AND status IN ('uploading', 'processing')`

	if _, err := r.q.Exec(ctx, query, id, reason); err != nil {
		return fmt.Errorf("repository: fail upload session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	if _, err := r.q.Exec(ctx, `DELETE FROM upload_sessions WHERE id = $1`, id); err != nil {
		return fmt.Errorf("repository: delete upload session: %w", err)
	}
	return nil
}

func (r *Repository) DeleteDraftUpload(ctx context.Context, uploadID uuid.UUID) error {
	return r.InTx(ctx, func(tx *Repository) error {
		var used bool
		const check = `
			SELECT EXISTS (SELECT 1 FROM messages WHERE upload_id = $1)
			    OR EXISTS (SELECT 1 FROM message_uploads WHERE upload_id = $1)
			FROM uploads WHERE id = $1 FOR UPDATE`
		if err := tx.q.QueryRow(ctx, check, uploadID).Scan(&used); err != nil {
			if noRows(err) {
				return nil
			}
			return fmt.Errorf("repository: check draft upload: %w", err)
		}
		if used {
			return domain.ErrUploadInUse
		}
		if _, err := tx.q.Exec(ctx, `DELETE FROM uploads WHERE id = $1`, uploadID); err != nil {
			return fmt.Errorf("repository: delete draft upload: %w", err)
		}
		return nil
	})
}

func (r *Repository) FileBySource(ctx context.Context, mode domain.UploadMode, source []byte) (domain.StoredFile, bool, error) {
	const query = `
		UPDATE files f
		SET linked_at = now()
		FROM file_sources s
		WHERE s.mode = $1 AND s.source_sha256 = $2 AND f.id = s.file_id
		RETURNING f.id, f.kind, f.sha256, f.payload, f.object_keys, f.size_bytes`

	file, err := scanFile(r.q.QueryRow(ctx, query, string(mode), source))
	if err != nil {
		if noRows(err) {
			return domain.StoredFile{}, false, nil
		}
		return domain.StoredFile{}, false, fmt.Errorf("repository: file by source: %w", err)
	}
	return file, true, nil
}

func (r *Repository) SaveFile(
	ctx context.Context,
	file domain.StoredFile,
	mode domain.UploadMode,
	source []byte,
) (domain.StoredFile, bool, error) {
	var (
		saved   domain.StoredFile
		created bool
	)
	err := r.InTx(ctx, func(tx *Repository) error {
		const upsert = `
			INSERT INTO files (id, kind, sha256, payload, object_keys, size_bytes)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
			ON CONFLICT ON CONSTRAINT files_content_key DO UPDATE SET linked_at = now()
			RETURNING id, kind, sha256, payload, object_keys, size_bytes`

		stored, err := scanFile(tx.q.QueryRow(ctx, upsert,
			file.ID, string(file.Kind), file.SHA256, []byte(file.Payload), file.ObjectKeys, file.SizeBytes))
		if err != nil {
			return fmt.Errorf("repository: save file: %w", err)
		}

		const remember = `
			INSERT INTO file_sources (mode, source_sha256, file_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (mode, source_sha256) DO NOTHING`
		if _, err := tx.q.Exec(ctx, remember, string(mode), source, stored.ID); err != nil {
			return fmt.Errorf("repository: remember file source: %w", err)
		}

		saved, created = stored, stored.ID == file.ID
		return nil
	})
	if err != nil {
		return domain.StoredFile{}, false, err
	}
	return saved, created, nil
}

func scanFile(row scannable) (domain.StoredFile, error) {
	var (
		f       domain.StoredFile
		kind    string
		payload []byte
	)
	if err := row.Scan(&f.ID, &kind, &f.SHA256, &payload, &f.ObjectKeys, &f.SizeBytes); err != nil {
		return domain.StoredFile{}, err
	}
	f.Kind = domain.MessageKind(kind)
	f.Payload = json.RawMessage(payload)
	return f, nil
}

func (r *Repository) StaleSessions(ctx context.Context, olderThan time.Time, limit int) ([]domain.UploadSession, error) {
	const query = `
		SELECT ` + sessionColumns + `
		FROM upload_sessions
		WHERE updated_at < $1
		ORDER BY updated_at
		LIMIT $2`

	rows, err := r.q.Query(ctx, query, olderThan, limit)
	if err != nil {
		return nil, fmt.Errorf("repository: stale upload sessions: %w", err)
	}
	defer rows.Close()

	var sessions []domain.UploadSession
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("repository: scan stale upload session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: stale upload sessions: %w", err)
	}
	return sessions, nil
}

func (r *Repository) FailStuckSessions(ctx context.Context, claimedBefore time.Time) (int, error) {
	const query = `
		UPDATE upload_sessions
		SET status = 'failed', failure = 'processing did not finish', claimed_at = NULL, updated_at = now()
		WHERE status = 'processing' AND claimed_at < $1`

	tag, err := r.q.Exec(ctx, query, claimedBefore)
	if err != nil {
		return 0, fmt.Errorf("repository: fail stuck upload sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

const unlinkedFile = `NOT EXISTS (SELECT 1 FROM uploads u WHERE u.file_id = f.id)`

func (r *Repository) SweepOrphanFiles(
	ctx context.Context,
	linkedBefore time.Time,
	limit int,
	remove func(context.Context, []domain.StoredFile) ([]uuid.UUID, error),
) (int, error) {
	var swept int
	err := r.InTx(ctx, func(tx *Repository) error {
		const pick = `
			SELECT f.id, f.kind, f.sha256, f.payload, f.object_keys, f.size_bytes
			FROM files f
			WHERE f.linked_at < $1 AND ` + unlinkedFile + `
			ORDER BY f.linked_at
			LIMIT $2
			FOR UPDATE OF f SKIP LOCKED`

		rows, err := tx.q.Query(ctx, pick, linkedBefore, limit)
		if err != nil {
			return fmt.Errorf("repository: pick orphan files: %w", err)
		}
		var orphans []domain.StoredFile
		for rows.Next() {
			f, err := scanFile(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("repository: scan orphan file: %w", err)
			}
			orphans = append(orphans, f)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("repository: pick orphan files: %w", err)
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

		const drop = `DELETE FROM files f WHERE f.id = ANY($1) AND f.linked_at < $2 AND ` + unlinkedFile
		tag, err := tx.q.Exec(ctx, drop, ids, linkedBefore)
		if err != nil {
			return fmt.Errorf("repository: delete orphan files: %w", err)
		}
		swept = int(tag.RowsAffected())
		return nil
	})
	if err != nil {
		return 0, err
	}
	return swept, nil
}

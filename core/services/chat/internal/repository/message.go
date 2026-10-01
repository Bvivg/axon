package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const messageColumns = `id, room_id, author_id, body, client_id, seq, sent_at, kind, payload, reply_to_id, forwarded_from_id,
	forward_origin_author_id, upload_id, edited_at, deleted_at`

const messageWithReply = `
	SELECT m.id, m.room_id, m.author_id, m.body, m.client_id, m.seq, m.sent_at, m.kind, m.payload,
	       m.reply_to_id, m.forwarded_from_id, m.forward_origin_author_id, m.upload_id, m.edited_at, m.deleted_at,
	       r.author_id, r.kind, left(r.body, 140), r.deleted_at
	FROM messages m
	LEFT JOIN messages r ON r.id = m.reply_to_id`

func (r *Repository) AppendMessage(ctx context.Context, m domain.Message) (domain.Message, bool, error) {
	var (
		stored    domain.Message
		duplicate bool
	)

	err := r.InTx(ctx, func(tx *Repository) error {
		if m.ClientID != "" {
			existing, err := tx.messageByClientID(ctx, m.RoomID, m.AuthorID, m.ClientID)
			switch {
			case err == nil:
				stored, duplicate = existing, true
				return nil
			case !errors.Is(err, domain.ErrMessageNotFound):
				return err
			}
		}

		seq, err := tx.nextSeq(ctx, m.RoomID)
		if err != nil {
			return err
		}

		const query = `
			INSERT INTO messages (id, room_id, author_id, body, client_id, seq, kind, payload, reply_to_id, forwarded_from_id,
			                      forward_origin_author_id, upload_id)
			VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF($7, ''), 'text'), COALESCE($8::jsonb, '{}'::jsonb), $9, $10, $11, $12)
			ON CONFLICT (room_id, author_id, client_id) WHERE client_id <> '' DO NOTHING
			RETURNING ` + messageColumns

		row := tx.q.QueryRow(ctx, query,
			m.ID, m.RoomID, m.AuthorID, m.Body, m.ClientID, seq,
			string(m.Kind), []byte(m.Payload), m.ReplyToID, m.ForwardedFromID,
			m.ForwardOriginAuthorID, m.UploadID)

		stored, err = scanMessage(row)
		switch {
		case err == nil:
		case isForeignKeyViolation(err, "messages_upload_id_fkey"):
			return domain.ErrUploadNotFound
		case !noRows(err):
			return fmt.Errorf("repository: append message: %w", err)
		default:
			existing, err := tx.messageByClientID(ctx, m.RoomID, m.AuthorID, m.ClientID)
			if err != nil {
				return err
			}
			stored, duplicate = existing, true
			return nil
		}

		revealed, err := tx.revealRoom(ctx, m.RoomID)
		if err != nil {
			return err
		}
		stored.Revealed = revealed

		return tx.advanceReadSeq(ctx, m.RoomID, m.AuthorID, stored.Seq)
	})
	if err != nil {
		return domain.Message{}, false, err
	}

	return stored, duplicate, nil
}

func (r *Repository) advanceReadSeq(ctx context.Context, roomID, userID uuid.UUID, seq int64) error {
	const query = `
		UPDATE room_members
		SET last_read_seq = GREATEST(last_read_seq, $3)
		WHERE room_id = $1 AND user_id = $2`

	if _, err := r.q.Exec(ctx, query, roomID, userID, seq); err != nil {
		return fmt.Errorf("repository: advance read position: %w", err)
	}
	return nil
}

func (r *Repository) revealRoom(ctx context.Context, roomID uuid.UUID) ([]uuid.UUID, error) {
	const query = `
		UPDATE room_members
		SET hidden_at = NULL
		WHERE room_id = $1 AND hidden_at IS NOT NULL
		RETURNING user_id`

	rows, err := r.q.Query(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("repository: reveal room: %w", err)
	}
	defer rows.Close()

	var revealed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("repository: scan revealed member: %w", err)
		}
		revealed = append(revealed, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: reveal room: %w", err)
	}
	return revealed, nil
}

func (r *Repository) nextSeq(ctx context.Context, roomID uuid.UUID) (int64, error) {
	const query = `
		UPDATE rooms
		SET next_seq = next_seq + 1
		WHERE id = $1
		RETURNING next_seq - 1`

	var seq int64
	if err := r.q.QueryRow(ctx, query, roomID).Scan(&seq); err != nil {
		if noRows(err) {
			return 0, domain.ErrRoomNotFound
		}
		return 0, fmt.Errorf("repository: allocate message position: %w", err)
	}
	return seq, nil
}

func (r *Repository) messageByClientID(
	ctx context.Context,
	roomID, authorID uuid.UUID,
	clientID string,
) (domain.Message, error) {
	const query = messageWithReply + `
		WHERE m.room_id = $1 AND m.author_id = $2 AND m.client_id = $3`

	m, err := scanMessageWithReply(r.q.QueryRow(ctx, query, roomID, authorID, clientID))
	if err != nil {
		if noRows(err) {
			return domain.Message{}, domain.ErrMessageNotFound
		}
		return domain.Message{}, fmt.Errorf("repository: message by client id: %w", err)
	}
	return m, nil
}

func (r *Repository) MessageByID(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	const query = messageWithReply + ` WHERE m.id = $1`

	m, err := scanMessageWithReply(r.q.QueryRow(ctx, query, id))
	if err != nil {
		if noRows(err) {
			return domain.Message{}, domain.ErrMessageNotFound
		}
		return domain.Message{}, fmt.Errorf("repository: message by id: %w", err)
	}
	return m, nil
}

func (r *Repository) ListMessages(
	ctx context.Context,
	roomID, callerID uuid.UUID,
	page domain.Page,
) ([]domain.Message, bool, error) {

	limit := page.Limit + 1

	kinds := make([]string, 0, len(page.Kinds))
	for _, kind := range page.Kinds {
		kinds = append(kinds, string(kind))
	}

	var (
		query string
		args  []any
	)

	if page.Backward() {
		query = messageWithReply + `
			JOIN room_members rm ON rm.room_id = m.room_id AND rm.user_id = $4
			WHERE m.room_id = $1 AND m.seq > rm.cleared_through_seq AND ($2 = 0 OR m.seq < $2)
				AND (cardinality($5::text[]) = 0 OR (m.kind = ANY($5::text[]) AND m.deleted_at IS NULL))
			ORDER BY m.seq DESC
			LIMIT $3`
		args = []any{roomID, page.BeforeSeq, limit, callerID, kinds}
	} else {
		query = messageWithReply + `
			JOIN room_members rm ON rm.room_id = m.room_id AND rm.user_id = $4
			WHERE m.room_id = $1 AND m.seq > rm.cleared_through_seq AND m.seq > $2
				AND (cardinality($5::text[]) = 0 OR (m.kind = ANY($5::text[]) AND m.deleted_at IS NULL))
			ORDER BY m.seq ASC
			LIMIT $3`
		args = []any{roomID, page.AfterSeq, limit, callerID, kinds}
	}

	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("repository: list messages: %w", err)
	}
	defer rows.Close()

	messages := make([]domain.Message, 0, page.Limit)
	for rows.Next() {
		m, err := scanMessageWithReply(rows)
		if err != nil {
			return nil, false, fmt.Errorf("repository: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("repository: list messages: %w", err)
	}

	hasMore := len(messages) > page.Limit
	if hasMore {
		messages = messages[:page.Limit]
	}

	if page.Backward() {
		reverse(messages)
	}

	return messages, hasMore, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (r *Repository) EditMessage(ctx context.Context, id uuid.UUID, body string) (domain.Message, error) {
	const query = `
		UPDATE messages
		SET body = $2, edited_at = now()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.q.Exec(ctx, query, id, body)
	if err != nil {
		return domain.Message{}, fmt.Errorf("repository: edit message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Message{}, domain.ErrMessageDeleted
	}
	return r.MessageByID(ctx, id)
}

func (r *Repository) DeleteMessage(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	const query = `
		UPDATE messages
		SET body = '', payload = '{}'::jsonb, deleted_at = now()
		WHERE id = $1 AND deleted_at IS NULL`

	tag, err := r.q.Exec(ctx, query, id)
	if err != nil {
		return domain.Message{}, fmt.Errorf("repository: delete message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Message{}, domain.ErrMessageDeleted
	}
	return r.MessageByID(ctx, id)
}

func messageTargets(m *domain.Message, kind *string, payload *[]byte) []any {
	return []any{
		&m.ID, &m.RoomID, &m.AuthorID, &m.Body, &m.ClientID, &m.Seq, &m.SentAt,
		kind, payload, &m.ReplyToID, &m.ForwardedFromID,
		&m.ForwardOriginAuthorID, &m.UploadID, &m.EditedAt, &m.DeletedAt,
	}
}

func scanMessage(row scannable) (domain.Message, error) {
	var (
		m       domain.Message
		kind    string
		payload []byte
	)
	if err := row.Scan(messageTargets(&m, &kind, &payload)...); err != nil {
		return domain.Message{}, err
	}

	m.Kind = domain.MessageKind(kind)
	m.Payload = json.RawMessage(payload)
	return m, nil
}

func scanMessageWithReply(row scannable) (domain.Message, error) {
	var (
		m       domain.Message
		kind    string
		payload []byte

		replyAuthor  *uuid.UUID
		replyKind    *string
		replyBody    *string
		replyDeleted *time.Time
	)
	targets := append(messageTargets(&m, &kind, &payload), &replyAuthor, &replyKind, &replyBody, &replyDeleted)
	if err := row.Scan(targets...); err != nil {
		return domain.Message{}, err
	}

	m.Kind = domain.MessageKind(kind)
	m.Payload = json.RawMessage(payload)

	if m.ReplyToID != nil && replyAuthor != nil {
		preview := domain.ReplyPreview{ID: *m.ReplyToID, AuthorID: *replyAuthor, Deleted: replyDeleted != nil}
		if replyKind != nil {
			preview.Kind = domain.MessageKind(*replyKind)
		}
		if replyBody != nil && !preview.Deleted {
			preview.Body = *replyBody
		}
		m.ReplyTo = &preview
	}
	return m, nil
}

func reverse(messages []domain.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

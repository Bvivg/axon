package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const messageColumns = `id, room_id, author_id, body, client_id, seq, sent_at, kind, payload, reply_to_id, forwarded_from_id`

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
			INSERT INTO messages (id, room_id, author_id, body, client_id, seq, kind, payload, reply_to_id, forwarded_from_id)
			VALUES ($1, $2, $3, $4, $5, $6, COALESCE(NULLIF($7, ''), 'text'), COALESCE($8::jsonb, '{}'::jsonb), $9, $10)
			ON CONFLICT (room_id, author_id, client_id) WHERE client_id <> '' DO NOTHING
			RETURNING ` + messageColumns

		row := tx.q.QueryRow(ctx, query,
			m.ID, m.RoomID, m.AuthorID, m.Body, m.ClientID, seq,
			string(m.Kind), []byte(m.Payload), m.ReplyToID, m.ForwardedFromID)

		stored, err = scanMessage(row)
		switch {
		case err == nil:
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

		if err := tx.unhideRoomForSender(ctx, m.RoomID, m.AuthorID); err != nil {
			return err
		}

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

func (r *Repository) unhideRoomForSender(ctx context.Context, roomID, userID uuid.UUID) error {
	const query = `
		UPDATE room_members
		SET hidden_at = NULL
		WHERE room_id = $1 AND user_id = $2 AND hidden_at IS NOT NULL`

	if _, err := r.q.Exec(ctx, query, roomID, userID); err != nil {
		return fmt.Errorf("repository: unhide room for sender: %w", err)
	}
	return nil
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
	const query = `
		SELECT ` + messageColumns + `
		FROM messages
		WHERE room_id = $1 AND author_id = $2 AND client_id = $3`

	m, err := scanMessage(r.q.QueryRow(ctx, query, roomID, authorID, clientID))
	if err != nil {
		if noRows(err) {
			return domain.Message{}, domain.ErrMessageNotFound
		}
		return domain.Message{}, fmt.Errorf("repository: message by client id: %w", err)
	}
	return m, nil
}

func (r *Repository) MessageByID(ctx context.Context, id uuid.UUID) (domain.Message, error) {
	const query = `SELECT ` + messageColumns + ` FROM messages WHERE id = $1`

	m, err := scanMessage(r.q.QueryRow(ctx, query, id))
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
		query = `
			SELECT m.id, m.room_id, m.author_id, m.body, m.client_id, m.seq, m.sent_at, m.kind, m.payload, m.reply_to_id, m.forwarded_from_id
			FROM messages m
			JOIN room_members rm ON rm.room_id = m.room_id AND rm.user_id = $4
			WHERE m.room_id = $1 AND m.seq > rm.cleared_through_seq AND ($2 = 0 OR m.seq < $2)
				AND (cardinality($5::text[]) = 0 OR m.kind = ANY($5::text[]))
			ORDER BY m.seq DESC
			LIMIT $3`
		args = []any{roomID, page.BeforeSeq, limit, callerID, kinds}
	} else {
		query = `
			SELECT m.id, m.room_id, m.author_id, m.body, m.client_id, m.seq, m.sent_at, m.kind, m.payload, m.reply_to_id, m.forwarded_from_id
			FROM messages m
			JOIN room_members rm ON rm.room_id = m.room_id AND rm.user_id = $4
			WHERE m.room_id = $1 AND m.seq > rm.cleared_through_seq AND m.seq > $2
				AND (cardinality($5::text[]) = 0 OR m.kind = ANY($5::text[]))
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
		m, err := scanMessage(rows)
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

func scanMessage(row scannable) (domain.Message, error) {
	var (
		m       domain.Message
		kind    string
		payload []byte
	)
	err := row.Scan(
		&m.ID, &m.RoomID, &m.AuthorID, &m.Body, &m.ClientID, &m.Seq, &m.SentAt,
		&kind, &payload, &m.ReplyToID, &m.ForwardedFromID,
	)
	if err != nil {
		return domain.Message{}, err
	}

	m.Kind = domain.MessageKind(kind)
	m.Payload = json.RawMessage(payload)
	return m, nil
}

func reverse(messages []domain.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

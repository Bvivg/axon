package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const messageColumns = `id, room_id, author_id, body, client_id, seq, sent_at`

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
			INSERT INTO messages (id, room_id, author_id, body, client_id, seq)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (room_id, author_id, client_id) WHERE client_id <> '' DO NOTHING
			RETURNING ` + messageColumns

		row := tx.q.QueryRow(ctx, query, m.ID, m.RoomID, m.AuthorID, m.Body, m.ClientID, seq)

		stored, err = scanMessage(row)
		switch {
		case err == nil:
			return nil
		case !noRows(err):
			return fmt.Errorf("repository: append message: %w", err)
		}

		existing, err := tx.messageByClientID(ctx, m.RoomID, m.AuthorID, m.ClientID)
		if err != nil {
			return err
		}
		stored, duplicate = existing, true
		return nil
	})
	if err != nil {
		return domain.Message{}, false, err
	}

	return stored, duplicate, nil
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

func (r *Repository) ListMessages(
	ctx context.Context,
	roomID uuid.UUID,
	page domain.Page,
) ([]domain.Message, bool, error) {

	limit := page.Limit + 1

	var (
		query string
		args  []any
	)

	if page.Backward() {
		query = `
			SELECT ` + messageColumns + `
			FROM messages
			WHERE room_id = $1 AND ($2 = 0 OR seq < $2)
			ORDER BY seq DESC
			LIMIT $3`
		args = []any{roomID, page.BeforeSeq, limit}
	} else {
		query = `
			SELECT ` + messageColumns + `
			FROM messages
			WHERE room_id = $1 AND seq > $2
			ORDER BY seq ASC
			LIMIT $3`
		args = []any{roomID, page.AfterSeq, limit}
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
	var m domain.Message
	err := row.Scan(&m.ID, &m.RoomID, &m.AuthorID, &m.Body, &m.ClientID, &m.Seq, &m.SentAt)
	return m, err
}

func reverse(messages []domain.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// messageColumns is the projection every message query shares, so the scan
// order is defined in exactly one place.
const messageColumns = `id, room_id, author_id, body, client_id, seq, sent_at`

// AppendMessage writes a message and returns it with the position it was given.
//
// The second return value reports that the message was already there: a client
// whose connection dropped between sending and being acknowledged resends, and
// the honest answer to that is the message it sent the first time, not a second
// copy of it. Recognising it needs the sender's own client id — without one,
// there is nothing to tell a resend apart from someone saying the same thing
// twice on purpose.
//
// The position comes from the room's counter rather than from a sequence, and
// the difference matters. A sequence hands out numbers without ordering the
// commits that use them: two senders can take 7 and 8, and 8 can land first.
// A client that had caught up to 8 would then never see 7 — a message lost in
// a way no error ever reports. Bumping the room's row takes a lock on it, so
// within one room the order positions are handed out in is the order they
// become visible in. The cost is that a room's writes serialise, which is what
// a room's message order means anyway.
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

		// DO NOTHING rather than letting the constraint raise: a raised
		// constraint aborts the transaction, and the answer to a resend is the
		// message that is already there — which would then need a second
		// transaction to go and read. The conflict target names the partial
		// index's predicate because that is what makes it the index Postgres
		// infers rather than an ambiguous one.
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

		// Nothing came back, so the insert conflicted: two copies of one resend
		// arrived at once and the other transaction wrote it. Its message is the
		// answer to both. The position taken above goes unused, which is fine —
		// positions only have to be ordered, not consecutive.
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

// nextSeq takes the room's next message position, locking the room row for the
// rest of the transaction.
//
// It doubles as the existence check for the room: no row updated means no room,
// and there is no point discovering that after the message is built.
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

// messageByClientID finds a message by the id its sender gave it.
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

// ListMessages reads a page of a room's history, oldest first, and reports
// whether more remain in the direction that was asked for.
//
// Both directions return the same order. A client appending to a transcript
// should not have to know whether the page came from paging back through
// history or from catching up after a reconnect.
func (r *Repository) ListMessages(
	ctx context.Context,
	roomID uuid.UUID,
	page domain.Page,
) ([]domain.Message, bool, error) {
	// One more than asked for: whether the extra row exists is the answer to
	// "is there more", and it costs one row rather than a second count query.
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

// scannable is what both a single row and a row within a result set satisfy.
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

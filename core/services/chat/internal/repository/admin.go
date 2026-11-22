package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authz"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/postgres"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const adminRoomSelect = `
	SELECT r.id, r.name, r.created_by, r.created_at, r.kind, r.direct_user_min, r.direct_user_max,
		(SELECT count(*) FROM room_members WHERE room_id = r.id),
		(SELECT sent_at FROM messages WHERE room_id = r.id ORDER BY seq DESC LIMIT 1),
		r.next_seq - 1
	FROM rooms r`

type roomCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

func scanAdminRoom(row scannable) (domain.AdminRoom, error) {
	var (
		out  domain.AdminRoom
		kind string
	)
	err := row.Scan(&out.Room.ID, &out.Room.Name, &out.Room.CreatedBy, &out.Room.CreatedAt, &kind,
		&out.Room.DirectUserMin, &out.Room.DirectUserMax, &out.Room.MemberCount, &out.LastActivity, &out.LastSeq)
	out.Room.Kind = domain.RoomKind(kind)
	return out, err
}

func (r *Repository) AdminRooms(ctx context.Context, f domain.AdminRoomFilter) ([]domain.AdminRoom, string, int64, error) {
	var (
		clauses []string
		args    []any
	)
	add := func(clause string, values ...any) {
		for _, v := range values {
			args = append(args, v)
			clause = strings.Replace(clause, "?", "$"+strconv.Itoa(len(args)), 1)
		}
		clauses = append(clauses, clause)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		add("r.name ILIKE ?", postgres.Containing(q))
	}
	if f.Kind != "" {
		add("r.kind = ?", string(f.Kind))
	}
	if f.Member != nil {
		add("EXISTS (SELECT 1 FROM room_members m WHERE m.room_id = r.id AND m.user_id = ?)", *f.Member)
	}
	where := func() string {
		if len(clauses) == 0 {
			return ""
		}
		return " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int64
	if err := r.q.QueryRow(ctx, `SELECT count(*) FROM rooms r`+where(), args...).Scan(&total); err != nil {
		return nil, "", 0, fmt.Errorf("repository: count rooms: %w", err)
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c roomCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", 0, &domain.ValidationError{Field: "cursor", Reason: "is not valid"}
		}
		add("(r.created_at, r.id) < (?, ?)", c.At, c.ID)
	}
	args = append(args, f.Limit+1)
	rows, err := r.q.Query(ctx, adminRoomSelect+where()+` ORDER BY r.created_at DESC, r.id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("repository: admin rooms: %w", err)
	}
	defer rows.Close()

	rooms := []domain.AdminRoom{}
	for rows.Next() {
		room, err := scanAdminRoom(rows)
		if err != nil {
			return nil, "", 0, fmt.Errorf("repository: scan admin room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, fmt.Errorf("repository: admin rooms: %w", err)
	}
	next := ""
	if len(rooms) > f.Limit {
		rooms = rooms[:f.Limit]
		last := rooms[f.Limit-1].Room
		raw, _ := json.Marshal(roomCursor{At: last.CreatedAt, ID: last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return rooms, next, total, nil
}

func (r *Repository) AdminRoom(ctx context.Context, id uuid.UUID) (domain.AdminRoom, error) {
	room, err := scanAdminRoom(r.q.QueryRow(ctx, adminRoomSelect+` WHERE r.id = $1`, id))
	if err != nil {
		if noRows(err) {
			return domain.AdminRoom{}, domain.ErrRoomNotFound
		}
		return domain.AdminRoom{}, fmt.Errorf("repository: admin room: %w", err)
	}
	return room, nil
}

func (r *Repository) AdminMessages(ctx context.Context, roomID uuid.UUID, beforeSeq int64, limit int) ([]domain.Message, bool, error) {
	const query = messageWithReply + `
		WHERE m.room_id = $1 AND ($2 = 0 OR m.seq < $2)
		ORDER BY m.seq DESC
		LIMIT $3`
	rows, err := r.q.Query(ctx, query, roomID, beforeSeq, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("repository: admin messages: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0, limit)
	for rows.Next() {
		m, err := scanMessageWithReply(rows)
		if err != nil {
			return nil, false, fmt.Errorf("repository: scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("repository: admin messages: %w", err)
	}
	more := len(messages) > limit
	if more {
		messages = messages[:limit]
	}
	reverse(messages)
	return messages, more, nil
}

func (r *Repository) RecordAdminAction(ctx context.Context, action authz.Action) error {
	if err := outbox.Write(ctx, r.q, OutboxTable, action.Event(uuid.New())); err != nil {
		return fmt.Errorf("repository: record admin action %s: %w", action.Name, err)
	}
	return nil
}

func (r *Repository) ModerateDeleteMessage(ctx context.Context, id uuid.UUID, action authz.Action) (domain.Message, error) {
	var deleted domain.Message
	err := r.InTx(ctx, func(tx *Repository) error {
		var err error
		if deleted, err = tx.DeleteMessage(ctx, id); err != nil {
			return err
		}
		return tx.RecordAdminAction(ctx, action)
	})
	return deleted, err
}

func (r *Repository) ModerateRemoveMember(ctx context.Context, roomID, userID uuid.UUID, action authz.Action) (*uuid.UUID, error) {
	var promoted *uuid.UUID
	err := r.InTx(ctx, func(tx *Repository) error {
		if err := tx.lockRoom(ctx, roomID); err != nil {
			return err
		}
		if err := tx.RemoveMember(ctx, roomID, userID); err != nil {
			return err
		}
		var err error
		if promoted, err = tx.PromoteOwnerIfNone(ctx, roomID); err != nil {
			return err
		}
		return tx.RecordAdminAction(ctx, action)
	})
	return promoted, err
}

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const roomColumns = `id, name, created_by, created_at, kind, direct_user_min, direct_user_max`

func scanRoom(row scannable) (domain.Room, error) {
	var (
		room    domain.Room
		kind    string
		minPeer *uuid.UUID
		maxPeer *uuid.UUID
	)

	err := row.Scan(&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &kind, &minPeer, &maxPeer)
	if err != nil {
		return domain.Room{}, err
	}

	room.Kind = domain.RoomKind(kind)
	room.DirectUserMin = minPeer
	room.DirectUserMax = maxPeer
	return room, nil
}

func (r *Repository) CreateRoom(ctx context.Context, room domain.Room, creator domain.Member) (domain.Room, error) {
	var created domain.Room

	err := r.InTx(ctx, func(tx *Repository) error {
		const query = `
			INSERT INTO rooms (id, name, created_by)
			VALUES ($1, $2, $3)
			RETURNING ` + roomColumns

		row := tx.q.QueryRow(ctx, query, room.ID, room.Name, room.CreatedBy)

		var err error
		created, err = scanRoom(row)
		if err != nil {
			return fmt.Errorf("repository: create room: %w", err)
		}

		if _, err := tx.AddMember(ctx, created.ID, creator); err != nil {
			return err
		}

		created.MemberCount = 1
		return nil
	})
	if err != nil {
		return domain.Room{}, err
	}

	return created, nil
}

func (r *Repository) RoomByID(ctx context.Context, id uuid.UUID) (domain.Room, error) {
	const query = `
		SELECT ` + roomColumns + `, (SELECT count(*) FROM room_members WHERE room_id = rooms.id)
		FROM rooms
		WHERE id = $1`

	row := r.q.QueryRow(ctx, query, id)

	var (
		room             domain.Room
		kind             string
		minPeer, maxPeer *uuid.UUID
	)
	err := row.Scan(
		&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &kind, &minPeer, &maxPeer, &room.MemberCount,
	)
	if err != nil {
		if noRows(err) {
			return domain.Room{}, domain.ErrRoomNotFound
		}
		return domain.Room{}, fmt.Errorf("repository: room by id: %w", err)
	}

	room.Kind = domain.RoomKind(kind)
	room.DirectUserMin = minPeer
	room.DirectUserMax = maxPeer
	return room, nil
}

func (r *Repository) RoomsForUser(ctx context.Context, userID uuid.UUID) ([]domain.Room, error) {
	const query = `
		SELECT r.id, r.name, r.created_by, r.created_at, r.kind, r.direct_user_min, r.direct_user_max,
		       (SELECT count(*) FROM room_members WHERE room_id = r.id),
		       (SELECT count(*) FROM messages msg
		        WHERE msg.room_id = r.id AND msg.seq > GREATEST(m.last_read_seq, m.cleared_through_seq)),
		       (SELECT COALESCE(max(o.last_read_seq), 0) FROM room_members o
		        WHERE o.room_id = r.id AND o.user_id <> $1)
		FROM rooms r
		JOIN room_members m ON m.room_id = r.id
		WHERE m.user_id = $1 AND m.hidden_at IS NULL
		ORDER BY r.created_at DESC, r.id`

	rows, err := r.q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: rooms for user: %w", err)
	}
	defer rows.Close()

	var rooms []domain.Room
	for rows.Next() {
		var (
			room             domain.Room
			kind             string
			minPeer, maxPeer *uuid.UUID
		)
		if err := rows.Scan(
			&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &kind, &minPeer, &maxPeer,
			&room.MemberCount, &room.UnreadCount, &room.OthersReadSeq,
		); err != nil {
			return nil, fmt.Errorf("repository: scan room: %w", err)
		}
		room.Kind = domain.RoomKind(kind)
		room.DirectUserMin = minPeer
		room.DirectUserMax = maxPeer
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: rooms for user: %w", err)
	}

	return rooms, nil
}

func (r *Repository) DirectRoomBetween(ctx context.Context, userA, userB uuid.UUID) (domain.Room, error) {
	min, max := domain.DirectPair(userA, userB)

	const query = `
		SELECT ` + roomColumns + `, (SELECT count(*) FROM room_members WHERE room_id = rooms.id)
		FROM rooms
		WHERE kind = 'direct' AND direct_user_min = $1 AND direct_user_max = $2`

	row := r.q.QueryRow(ctx, query, min, max)

	var (
		room             domain.Room
		kind             string
		minPeer, maxPeer *uuid.UUID
	)
	err := row.Scan(
		&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &kind, &minPeer, &maxPeer, &room.MemberCount,
	)
	if err != nil {
		if noRows(err) {
			return domain.Room{}, domain.ErrRoomNotFound
		}
		return domain.Room{}, fmt.Errorf("repository: direct room between: %w", err)
	}

	room.Kind = domain.RoomKind(kind)
	room.DirectUserMin = minPeer
	room.DirectUserMax = maxPeer
	return room, nil
}

func (r *Repository) GetOrCreateDirectRoom(
	ctx context.Context,
	newRoom domain.Room,
	memberA, memberB domain.Member,
) (domain.Room, error) {
	min, max := domain.DirectPair(memberA.UserID, memberB.UserID)

	existing, err := r.DirectRoomBetween(ctx, min, max)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, domain.ErrRoomNotFound):
		return domain.Room{}, err
	}

	var created domain.Room
	err = r.InTx(ctx, func(tx *Repository) error {
		const query = `
			INSERT INTO rooms (id, name, created_by, kind, direct_user_min, direct_user_max)
			VALUES ($1, $2, $3, 'direct', $4, $5)
			RETURNING ` + roomColumns

		row := tx.q.QueryRow(ctx, query, newRoom.ID, newRoom.Name, newRoom.CreatedBy, min, max)

		var scanErr error
		created, scanErr = scanRoom(row)
		if scanErr != nil {
			return fmt.Errorf("repository: create direct room: %w", scanErr)
		}

		if _, err := tx.AddMember(ctx, created.ID, memberA); err != nil {
			return err
		}
		if _, err := tx.AddMember(ctx, created.ID, memberB); err != nil {
			return err
		}

		created.MemberCount = 2
		return nil
	})
	if err != nil {
		if isUniqueViolation(err, "rooms_direct_pair_key") {
			return r.DirectRoomBetween(ctx, min, max)
		}
		return domain.Room{}, err
	}

	return created, nil
}

func (r *Repository) AddMember(ctx context.Context, roomID uuid.UUID, m domain.Member) (bool, error) {
	const query = `
		INSERT INTO room_members (room_id, user_id, display_name, role)
		VALUES ($1, $2, $3, COALESCE(NULLIF($4, ''), 'member'))
		ON CONFLICT (room_id, user_id) DO UPDATE
			SET display_name = CASE WHEN EXCLUDED.display_name = '' THEN room_members.display_name ELSE EXCLUDED.display_name END
		RETURNING (xmax = 0)`

	var inserted bool
	err := r.q.QueryRow(ctx, query, roomID, m.UserID, m.DisplayName, string(m.Role)).Scan(&inserted)
	if err != nil {
		return false, fmt.Errorf("repository: add member: %w", err)
	}
	return inserted, nil
}

func (r *Repository) RemoveMember(ctx context.Context, roomID, userID uuid.UUID) error {
	const query = `DELETE FROM room_members WHERE room_id = $1 AND user_id = $2`

	if _, err := r.q.Exec(ctx, query, roomID, userID); err != nil {
		return fmt.Errorf("repository: remove member: %w", err)
	}
	return nil
}

func (r *Repository) Members(ctx context.Context, roomID uuid.UUID) ([]domain.Member, error) {
	const query = `
		SELECT user_id, display_name, joined_at, hidden_at, cleared_through_seq, last_read_seq, role
		FROM room_members
		WHERE room_id = $1
		ORDER BY joined_at, user_id`

	rows, err := r.q.Query(ctx, query, roomID)
	if err != nil {
		return nil, fmt.Errorf("repository: members: %w", err)
	}
	defer rows.Close()

	var members []domain.Member
	for rows.Next() {
		var (
			m    domain.Member
			role string
		)
		if err := rows.Scan(
			&m.UserID, &m.DisplayName, &m.JoinedAt, &m.HiddenAt, &m.ClearedThroughSeq, &m.LastReadSeq, &role,
		); err != nil {
			return nil, fmt.Errorf("repository: scan member: %w", err)
		}
		m.Role = domain.MemberRole(role)
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: members: %w", err)
	}

	return members, nil
}

func (r *Repository) MarkRead(ctx context.Context, roomID, userID uuid.UUID, seq int64) error {
	const query = `
		UPDATE room_members
		SET last_read_seq = GREATEST(last_read_seq, $3)
		WHERE room_id = $1 AND user_id = $2`

	tag, err := r.q.Exec(ctx, query, roomID, userID, seq)
	if err != nil {
		return fmt.Errorf("repository: mark read: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotAMember
	}
	return nil
}

func (r *Repository) IsMember(ctx context.Context, roomID, userID uuid.UUID) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`

	var member bool
	if err := r.q.QueryRow(ctx, query, roomID, userID).Scan(&member); err != nil {
		return false, fmt.Errorf("repository: is member: %w", err)
	}
	return member, nil
}

func (r *Repository) HideRoom(ctx context.Context, roomID, userID uuid.UUID) error {
	const query = `
		UPDATE room_members
		SET hidden_at = now(),
		    cleared_through_seq = COALESCE((SELECT max(seq) FROM messages WHERE room_id = $1), 0)
		WHERE room_id = $1 AND user_id = $2`

	tag, err := r.q.Exec(ctx, query, roomID, userID)
	if err != nil {
		return fmt.Errorf("repository: hide room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotAMember
	}
	return nil
}

func (r *Repository) CreateGroup(
	ctx context.Context,
	room domain.Room,
	owner domain.Member,
	members []domain.Member,
) (domain.Room, error) {
	var created domain.Room

	err := r.InTx(ctx, func(tx *Repository) error {
		const query = `
			INSERT INTO rooms (id, name, created_by, kind)
			VALUES ($1, $2, $3, 'group')
			RETURNING ` + roomColumns

		var err error
		created, err = scanRoom(tx.q.QueryRow(ctx, query, room.ID, room.Name, room.CreatedBy))
		if err != nil {
			return fmt.Errorf("repository: create group: %w", err)
		}

		owner.Role = domain.MemberRoleOwner
		if _, err := tx.AddMember(ctx, created.ID, owner); err != nil {
			return err
		}
		for _, m := range members {
			m.Role = domain.MemberRoleMember
			if _, err := tx.AddMember(ctx, created.ID, m); err != nil {
				return err
			}
		}

		created.MemberCount = 1 + len(members)
		return nil
	})
	if err != nil {
		return domain.Room{}, err
	}

	return created, nil
}

func (r *Repository) AddGroupMembers(
	ctx context.Context,
	roomID uuid.UUID,
	members []domain.Member,
	limit int,
) ([]uuid.UUID, error) {
	var added []uuid.UUID

	err := r.InTx(ctx, func(tx *Repository) error {
		if err := tx.lockRoom(ctx, roomID); err != nil {
			return err
		}

		var count int
		if err := tx.q.QueryRow(ctx, `SELECT count(*) FROM room_members WHERE room_id = $1`, roomID).Scan(&count); err != nil {
			return fmt.Errorf("repository: count group members: %w", err)
		}

		for _, m := range members {
			inserted, err := tx.AddMember(ctx, roomID, m)
			if err != nil {
				return err
			}
			if inserted {
				added = append(added, m.UserID)
			}
		}

		if count+len(added) > limit {
			return domain.ErrGroupFull
		}
		if len(added) == 0 {
			return nil
		}

		const startHistory = `
			UPDATE room_members
			SET cleared_through_seq = (SELECT COALESCE(max(seq), 0) FROM messages WHERE room_id = $1)
			WHERE room_id = $1 AND user_id = ANY($2)`

		if _, err := tx.q.Exec(ctx, startHistory, roomID, added); err != nil {
			return fmt.Errorf("repository: start history for added members: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return added, nil
}

func (r *Repository) RenameRoom(ctx context.Context, roomID uuid.UUID, name string) error {
	const query = `UPDATE rooms SET name = $2 WHERE id = $1`

	tag, err := r.q.Exec(ctx, query, roomID, name)
	if err != nil {
		return fmt.Errorf("repository: rename room: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoomNotFound
	}
	return nil
}

func (r *Repository) MemberRole(ctx context.Context, roomID, userID uuid.UUID) (domain.MemberRole, error) {
	const query = `SELECT role FROM room_members WHERE room_id = $1 AND user_id = $2`

	var role string
	if err := r.q.QueryRow(ctx, query, roomID, userID).Scan(&role); err != nil {
		if noRows(err) {
			return "", domain.ErrNotAMember
		}
		return "", fmt.Errorf("repository: member role: %w", err)
	}
	return domain.MemberRole(role), nil
}

func (r *Repository) LeaveGroup(ctx context.Context, roomID, userID uuid.UUID) (*uuid.UUID, error) {
	var promoted *uuid.UUID

	err := r.InTx(ctx, func(tx *Repository) error {
		if err := tx.lockRoom(ctx, roomID); err != nil {
			return err
		}
		if err := tx.RemoveMember(ctx, roomID, userID); err != nil {
			return err
		}

		var err error
		promoted, err = tx.PromoteOwnerIfNone(ctx, roomID)
		return err
	})
	if err != nil {
		return nil, err
	}

	return promoted, nil
}

func (r *Repository) lockRoom(ctx context.Context, roomID uuid.UUID) error {
	var locked uuid.UUID
	if err := r.q.QueryRow(ctx, `SELECT id FROM rooms WHERE id = $1 FOR NO KEY UPDATE`, roomID).Scan(&locked); err != nil {
		if noRows(err) {
			return domain.ErrRoomNotFound
		}
		return fmt.Errorf("repository: lock room: %w", err)
	}
	return nil
}

func (r *Repository) HistoryStart(ctx context.Context, roomID, userID uuid.UUID) (int64, error) {
	const query = `SELECT cleared_through_seq FROM room_members WHERE room_id = $1 AND user_id = $2`

	var seq int64
	if err := r.q.QueryRow(ctx, query, roomID, userID).Scan(&seq); err != nil {
		if noRows(err) {
			return 0, domain.ErrNotAMember
		}
		return 0, fmt.Errorf("repository: history start: %w", err)
	}
	return seq, nil
}

func (r *Repository) PromoteOwnerIfNone(ctx context.Context, roomID uuid.UUID) (*uuid.UUID, error) {
	const query = `
		UPDATE room_members
		SET role = 'owner'
		WHERE room_id = $1
			AND NOT EXISTS (SELECT 1 FROM room_members WHERE room_id = $1 AND role = 'owner')
			AND user_id = (
				SELECT user_id FROM room_members WHERE room_id = $1 ORDER BY joined_at, user_id LIMIT 1
			)
		RETURNING user_id`

	var promoted uuid.UUID
	if err := r.q.QueryRow(ctx, query, roomID).Scan(&promoted); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("repository: promote owner: %w", err)
	}
	return &promoted, nil
}

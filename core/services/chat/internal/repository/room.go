package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const roomColumns = `id, name, created_by, created_at`

func (r *Repository) CreateRoom(ctx context.Context, room domain.Room, creator domain.Member) (domain.Room, error) {
	var created domain.Room

	err := r.InTx(ctx, func(tx *Repository) error {
		const query = `
			INSERT INTO rooms (id, name, created_by)
			VALUES ($1, $2, $3)
			RETURNING ` + roomColumns

		row := tx.q.QueryRow(ctx, query, room.ID, room.Name, room.CreatedBy)
		if err := row.Scan(&created.ID, &created.Name, &created.CreatedBy, &created.CreatedAt); err != nil {
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

	var room domain.Room
	err := r.q.QueryRow(ctx, query, id).Scan(
		&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &room.MemberCount,
	)
	if err != nil {
		if noRows(err) {
			return domain.Room{}, domain.ErrRoomNotFound
		}
		return domain.Room{}, fmt.Errorf("repository: room by id: %w", err)
	}
	return room, nil
}

func (r *Repository) RoomsForUser(ctx context.Context, userID uuid.UUID) ([]domain.Room, error) {
	const query = `
		SELECT r.id, r.name, r.created_by, r.created_at,
		       (SELECT count(*) FROM room_members WHERE room_id = r.id)
		FROM rooms r
		JOIN room_members m ON m.room_id = r.id
		WHERE m.user_id = $1
		ORDER BY r.created_at DESC, r.id`

	rows, err := r.q.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("repository: rooms for user: %w", err)
	}
	defer rows.Close()

	var rooms []domain.Room
	for rows.Next() {
		var room domain.Room
		if err := rows.Scan(
			&room.ID, &room.Name, &room.CreatedBy, &room.CreatedAt, &room.MemberCount,
		); err != nil {
			return nil, fmt.Errorf("repository: scan room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: rooms for user: %w", err)
	}

	return rooms, nil
}

func (r *Repository) AddMember(ctx context.Context, roomID uuid.UUID, m domain.Member) (bool, error) {
	const query = `
		INSERT INTO room_members (room_id, user_id, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (room_id, user_id) DO UPDATE
			SET display_name = EXCLUDED.display_name
		RETURNING (xmax = 0)`

	var inserted bool
	err := r.q.QueryRow(ctx, query, roomID, m.UserID, m.DisplayName).Scan(&inserted)
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
		SELECT user_id, display_name, joined_at
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
		var m domain.Member
		if err := rows.Scan(&m.UserID, &m.DisplayName, &m.JoinedAt); err != nil {
			return nil, fmt.Errorf("repository: scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: members: %w", err)
	}

	return members, nil
}

func (r *Repository) IsMember(ctx context.Context, roomID, userID uuid.UUID) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2)`

	var member bool
	if err := r.q.QueryRow(ctx, query, roomID, userID).Scan(&member); err != nil {
		return false, fmt.Errorf("repository: is member: %w", err)
	}
	return member, nil
}

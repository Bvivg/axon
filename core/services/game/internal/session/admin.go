package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type AdminState int

const (
	AdminAny AdminState = iota
	AdminLive
	AdminWaiting
	AdminFinished
)

type AdminFilter struct {
	Game       engine.Key
	State      AdminState
	Player     *uuid.UUID
	Tournament *uuid.UUID
	Cursor     string
	Limit      int
}

type AdminPage struct {
	Sessions []Session
	Next     string
	Total    int64
}

var ErrInvalidCursor = errors.New("session: invalid cursor")

type adminCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

func (s *Store) AdminList(ctx context.Context, f AdminFilter) (AdminPage, error) {
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
	if f.Game != "" {
		add("game = ?", string(f.Game))
	}
	switch f.State {
	case AdminLive:
		add("opponent_id IS NOT NULL AND state->>'status' = 'in_progress'")
	case AdminWaiting:
		add("opponent_id IS NULL")
	case AdminFinished:
		add("opponent_id IS NOT NULL AND state->>'status' <> 'in_progress'")
	}
	if f.Player != nil {
		add("(creator_id = ? OR opponent_id = ?)", *f.Player, *f.Player)
	}
	if f.Tournament != nil {
		add("tournament_id = ?", *f.Tournament)
	}
	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game.sessions`+where, args...).Scan(&total); err != nil {
		return AdminPage{}, fmt.Errorf("session: count for admin: %w", err)
	}

	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c adminCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return AdminPage{}, ErrInvalidCursor
		}
		add("(updated_at, id) < (?, ?)", c.At, c.ID)
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, f.Limit+1)
	rows, err := s.pool.Query(ctx, `SELECT `+Columns+` FROM game.sessions`+where+
		` ORDER BY updated_at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return AdminPage{}, fmt.Errorf("session: list for admin: %w", err)
	}
	defer rows.Close()

	page := AdminPage{Total: total}
	for rows.Next() {
		item, err := Scan(rows)
		if err != nil {
			return AdminPage{}, fmt.Errorf("session: scan for admin: %w", err)
		}
		page.Sessions = append(page.Sessions, item)
	}
	if err := rows.Err(); err != nil {
		return AdminPage{}, fmt.Errorf("session: list for admin: %w", err)
	}
	if len(page.Sessions) > f.Limit {
		page.Sessions = page.Sessions[:f.Limit]
		last := page.Sessions[f.Limit-1]
		raw, _ := json.Marshal(adminCursor{At: last.UpdatedAt, ID: last.ID})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

const (
	AdminMoveLimit  = 2000
	AdminQueueLimit = 500
)

func (s *Store) AdminSession(ctx context.Context, id uuid.UUID, limit int) (Session, []MoveRecord, error) {
	var game Session
	var moves []MoveRecord
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		game, err = Scan(tx.QueryRow(ctx, `SELECT `+Columns+` FROM game.sessions WHERE id=$1`, id))
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (ply) ply, player_id, data, played_at FROM game.moves
			WHERE session_id=$1 AND ply<=$2 ORDER BY ply, played_at DESC LIMIT $3`, id, game.State.Ply, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var move MoveRecord
			var data []byte
			if err := rows.Scan(&move.Ply, &move.Player, &data, &move.PlayedAt); err != nil {
				return err
			}
			move.Data = data
			moves = append(moves, move)
		}
		return rows.Err()
	})
	if err != nil {
		return Session{}, nil, fmt.Errorf("session: admin session: %w", err)
	}
	return game, moves, nil
}

type QueueEntry struct {
	User       uuid.UUID
	Game       engine.Key
	Rating     int
	JoinedAt   time.Time
	LastSeenAt time.Time
	Session    *uuid.UUID
}

func (s *Store) MatchQueue(ctx context.Context, game engine.Key, limit int) ([]QueueEntry, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, game, rating, joined_at, last_seen_at, session_id FROM game.match_queue
		WHERE ($1 = '' OR game = $1)
		ORDER BY session_id IS NOT NULL, joined_at, user_id LIMIT $2`, string(game), limit)
	if err != nil {
		return nil, fmt.Errorf("session: match queue: %w", err)
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (QueueEntry, error) {
		var e QueueEntry
		err := row.Scan(&e.User, &e.Game, &e.Rating, &e.JoinedAt, &e.LastSeenAt, &e.Session)
		return e, err
	})
	if err != nil {
		return nil, fmt.Errorf("session: match queue: %w", err)
	}
	return entries, nil
}

func (s *Store) LeaveQueue(ctx context.Context, user uuid.UUID, game engine.Key) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		removed, err := dequeue(ctx, tx, user, game)
		if err != nil {
			return err
		}
		if !removed {
			return ErrNotSearching
		}
		_, err = tx.Exec(ctx, `SELECT pg_notify($1,$2)`, matchChannel, user.String())
		return err
	})
	if err != nil && !errors.Is(err, ErrNotSearching) {
		return fmt.Errorf("session: leave queue: %w", err)
	}
	return err
}

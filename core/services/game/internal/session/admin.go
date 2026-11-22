package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"

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
	List       adminquery.Request
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

const (
	liveSession     = "opponent_id IS NOT NULL AND state->>'status' = 'in_progress'"
	waitingSession  = "opponent_id IS NULL"
	finishedSession = "opponent_id IS NOT NULL AND state->>'status' <> 'in_progress'"
	adminOrder      = "updated_at DESC, id DESC"
)

var sessionSchema = adminquery.NewSchema(adminOrder, "id DESC",
	adminquery.Field{Name: "id", Column: "id", Kind: adminquery.UUID},
	adminquery.Field{Name: "game", Column: "game", Kind: adminquery.String},
	adminquery.Field{Name: "status", Kind: adminquery.Enum, Values: map[string]string{
		"live":     liveSession,
		"waiting":  waitingSession,
		"finished": finishedSession,
		"won":      "opponent_id IS NOT NULL AND state->>'status' = 'win'",
		"draw":     "opponent_id IS NOT NULL AND state->>'status' = 'draw'",
	}},
	adminquery.Field{Name: "player", Kind: adminquery.Custom, Build: playerCondition},
	adminquery.Field{Name: "tournament", Column: "tournament_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "rated", Column: "rated", Kind: adminquery.Bool},
	adminquery.Field{Name: "moves", Column: "COALESCE((state->>'ply')::integer, 0)", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "updated", Column: "updated_at", Kind: adminquery.Time, Sortable: true},
)

func playerCondition(op adminquery.Op, value string) (adminquery.Condition, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return adminquery.Condition{}, fmt.Errorf("needs a UUID for %q", "player")
	}
	switch op {
	case adminquery.OpEq, adminquery.OpHas:
		return adminquery.Condition{SQL: "(creator_id = ? OR opponent_id IS NOT DISTINCT FROM ?)", Args: []any{id, id}}, nil
	case adminquery.OpNe:
		return adminquery.Condition{SQL: "(creator_id <> ? AND opponent_id IS DISTINCT FROM ?)", Args: []any{id, id}}, nil
	}
	return adminquery.Condition{}, fmt.Errorf("does not support %q on %q", op, "player")
}

func (s *Store) AdminList(ctx context.Context, f AdminFilter) (AdminPage, error) {
	var where adminquery.Where
	if f.Game != "" {
		where.Add("game = ?", string(f.Game))
	}
	switch f.State {
	case AdminLive:
		where.Add(liveSession)
	case AdminWaiting:
		where.Add(waitingSession)
	case AdminFinished:
		where.Add(finishedSession)
	}
	if f.Player != nil {
		where.Add("(creator_id = ? OR opponent_id = ?)", *f.Player, *f.Player)
	}
	if f.Tournament != nil {
		where.Add("tournament_id = ?", *f.Tournament)
	}
	listed, err := sessionSchema.Prepare(f.List, &where)
	if err != nil {
		return AdminPage{}, err
	}

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game.sessions`+where.SQL(), where.Args()...).Scan(&total); err != nil {
		return AdminPage{}, fmt.Errorf("session: count for admin: %w", err)
	}

	order, limit, paging := adminOrder, f.Limit, ""
	if f.List.Paged() {
		var offset int
		limit, offset = f.List.Page()
		order, paging = listed.Order, where.Paginate(limit, offset)
	} else {
		if f.Cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
			var c adminCursor
			if err != nil || json.Unmarshal(raw, &c) != nil {
				return AdminPage{}, ErrInvalidCursor
			}
			where.Add("(updated_at, id) < (?, ?)", c.At, c.ID)
		}
		paging = " LIMIT " + where.Arg(limit+1)
	}
	rows, err := s.pool.Query(ctx, `SELECT `+Columns+` FROM game.sessions`+where.SQL()+` ORDER BY `+order+paging, where.Args()...)
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
	if !f.List.Paged() && len(page.Sessions) > limit {
		page.Sessions = page.Sessions[:limit]
		last := page.Sessions[limit-1]
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

type QueueFilter struct {
	Game  engine.Key
	Limit int
	List  adminquery.Request
}

type QueuePage struct {
	Entries []QueueEntry
	Total   *int64
}

const queueOrder = "session_id IS NOT NULL, joined_at, user_id"

var queueSchema = adminquery.NewSchema(queueOrder+", game", "user_id, game",
	adminquery.Field{Name: "user", Column: "user_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "game", Column: "game", Kind: adminquery.String},
	adminquery.Field{Name: "rating", Column: "rating", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "joined", Column: "joined_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "matched", Column: "(session_id IS NOT NULL)", Kind: adminquery.Bool},
)

func (s *Store) MatchQueue(ctx context.Context, f QueueFilter) (QueuePage, error) {
	var where adminquery.Where
	if f.Game != "" {
		where.Add("game = ?", string(f.Game))
	}
	listed, err := queueSchema.Prepare(f.List, &where)
	if err != nil {
		return QueuePage{}, err
	}
	var page QueuePage
	order, paging := queueOrder, ""
	if f.List.Paged() {
		var total int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game.match_queue`+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return QueuePage{}, fmt.Errorf("session: count match queue: %w", err)
		}
		page.Total = &total
		limit, offset := f.List.Page()
		order, paging = listed.Order, where.Paginate(limit, offset)
	} else {
		paging = " LIMIT " + where.Arg(f.Limit)
	}
	rows, err := s.pool.Query(ctx, `SELECT user_id, game, rating, joined_at, last_seen_at, session_id FROM game.match_queue`+
		where.SQL()+` ORDER BY `+order+paging, where.Args()...)
	if err != nil {
		return QueuePage{}, fmt.Errorf("session: match queue: %w", err)
	}
	page.Entries, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (QueueEntry, error) {
		var e QueueEntry
		err := row.Scan(&e.User, &e.Game, &e.Rating, &e.JoinedAt, &e.LastSeenAt, &e.Session)
		return e, err
	})
	if err != nil {
		return QueuePage{}, fmt.Errorf("session: match queue: %w", err)
	}
	return page, nil
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

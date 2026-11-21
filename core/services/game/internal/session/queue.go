package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/matchmaking"
)

var ErrNotSearching = errors.New("not searching for a match")

type Ticket struct {
	User     uuid.UUID
	Game     engine.Key
	Rating   int
	JoinedAt time.Time
	Now      time.Time
	Wait     time.Duration
	Session  *uuid.UUID
}

func (t Ticket) Window() int {
	return matchmaking.Window(t.Now.Sub(t.JoinedAt), t.Wait)
}

const ticketQuery = `SELECT q.rating, q.joined_at, q.session_id, q.started, s.state->>'status', clock_timestamp()
	FROM game.match_queue q LEFT JOIN game.sessions s ON s.id = q.session_id
	WHERE q.user_id=$1 AND q.game=$2`

type queued struct {
	ticket  Ticket
	status  *string
	started bool
}

func (q queued) finished() bool {
	return q.ticket.Session != nil && q.status != nil && *q.status != string(engine.StatusInProgress)
}

func readTicket(ctx context.Context, tx pgx.Tx, user uuid.UUID, game engine.Key, lock bool) (queued, error) {
	query := ticketQuery
	if lock {
		query += " FOR UPDATE OF q"
	}
	item := queued{ticket: Ticket{User: user, Game: game}}
	err := tx.QueryRow(ctx, query, user, game).Scan(&item.ticket.Rating, &item.ticket.JoinedAt, &item.ticket.Session, &item.started, &item.status, &item.ticket.Now)
	if errors.Is(err, pgx.ErrNoRows) {
		return queued{}, ErrNotSearching
	}
	if !item.started {
		item.ticket.Session = nil
	}
	return item, err
}

func active(ctx context.Context, tx pgx.Tx) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM game.match_queue WHERE session_id IS NULL AND last_seen_at > clock_timestamp() - interval '1 minute') +
		(SELECT count(*) * 2 FROM game.sessions WHERE opponent_id IS NOT NULL AND state->>'status' = 'in_progress' AND updated_at > clock_timestamp() - interval '10 minutes')`).Scan(&count)
	return count, err
}

func (s *Store) withTicket(ctx context.Context, user uuid.UUID, game engine.Key, run func(pgx.Tx) (queued, error)) (Ticket, error) {
	if _, err := s.games.Get(game); err != nil {
		return Ticket{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Ticket{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item, err := run(tx)
	if errors.Is(err, ErrNotSearching) {
		if err := tx.Commit(ctx); err != nil {
			return Ticket{}, err
		}
		return Ticket{}, ErrNotSearching
	}
	if err != nil {
		return Ticket{}, err
	}
	players, err := active(ctx, tx)
	if err != nil {
		return Ticket{}, err
	}
	item.ticket.Wait = matchmaking.Wait(players)
	return item.ticket, tx.Commit(ctx)
}

func (s *Store) FindMatch(ctx context.Context, user, request uuid.UUID, game engine.Key, table Table) (Ticket, error) {
	seats := max(table.Seats, DefaultSeats)
	if err := s.games.ValidateTable(game, seats, table.Options); err != nil {
		return Ticket{}, fmt.Errorf("%w: %w", ErrInvalidTable, err)
	}
	var options any
	if len(table.Options) > 0 {
		options = []byte(table.Options)
	}
	return s.withTicket(ctx, user, game, func(tx pgx.Tx) (queued, error) {
		item, err := readTicket(ctx, tx, user, game, true)
		switch {
		case err == nil && !item.finished():
			_, err = tx.Exec(ctx, `UPDATE game.match_queue SET last_seen_at=clock_timestamp() WHERE user_id=$1 AND game=$2`, user, game)
			return item, err
		case err != nil && !errors.Is(err, ErrNotSearching):
			return queued{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO game.ratings (user_id, game) VALUES ($1,$2) ON CONFLICT DO NOTHING`, user, game); err != nil {
			return queued{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO game.match_queue (user_id, game, rating, request_id, seats, options, options_key)
			SELECT $1, $2, rating, $3, $4, $5, $6 FROM game.ratings WHERE user_id=$1 AND game=$2
			ON CONFLICT (user_id, game) DO UPDATE SET rating=EXCLUDED.rating, request_id=EXCLUDED.request_id,
				joined_at=clock_timestamp(), last_seen_at=clock_timestamp(), session_id=NULL,
				seats=EXCLUDED.seats, options=EXCLUDED.options, options_key=EXCLUDED.options_key`, user, game, request, seats, options, string(table.Options)); err != nil {
			return queued{}, err
		}
		return readTicket(ctx, tx, user, game, false)
	})
}

func (s *Store) Ticket(ctx context.Context, user uuid.UUID, game engine.Key) (Ticket, error) {
	return s.withTicket(ctx, user, game, func(tx pgx.Tx) (queued, error) {
		item, err := readTicket(ctx, tx, user, game, false)
		if err == nil && item.finished() {
			return queued{}, ErrNotSearching
		}
		return item, err
	})
}

func (s *Store) Touch(ctx context.Context, user uuid.UUID, game engine.Key) error {
	_, err := s.pool.Exec(ctx, `UPDATE game.match_queue SET last_seen_at=clock_timestamp() WHERE user_id=$1 AND game=$2 AND session_id IS NULL`, user, game)
	return err
}

func (s *Store) CancelMatch(ctx context.Context, user uuid.UUID, game engine.Key) (Ticket, error) {
	return s.withTicket(ctx, user, game, func(tx pgx.Tx) (queued, error) {
		item, err := readTicket(ctx, tx, user, game, true)
		if err != nil {
			return queued{}, err
		}
		if item.ticket.Session != nil {
			if item.finished() {
				return queued{}, ErrNotSearching
			}
			return item, nil
		}
		if _, err := tx.Exec(ctx, `DELETE FROM game.match_queue WHERE user_id=$1 AND game=$2 AND session_id IS NULL`, user, game); err != nil {
			return queued{}, err
		}
		return queued{}, ErrNotSearching
	})
}

func (s *Store) MatchRound(ctx context.Context) ([]Pairing, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('game.matchmaking'))`).Scan(&locked); err != nil || !locked {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM game.match_queue WHERE session_id IS NULL AND last_seen_at < clock_timestamp() - make_interval(secs => $1)`, matchmaking.AbandonAfter.Seconds()); err != nil {
		return nil, err
	}
	players, err := active(ctx, tx)
	if err != nil {
		return nil, err
	}
	wait := matchmaking.Wait(players)
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT user_id, game, rating, joined_at, seats, options_key, options FROM game.match_queue
		WHERE session_id IS NULL AND last_seen_at >= $1 ORDER BY game, seats, options_key, joined_at FOR UPDATE`, now.Add(-matchmaking.StaleAfter))
	if err != nil {
		return nil, err
	}
	queues := map[queueKey][]matchmaking.Entry{}
	tables := map[queueKey]Table{}
	var order []queueKey
	for rows.Next() {
		var entry matchmaking.Entry
		var key queueKey
		var options []byte
		if err := rows.Scan(&entry.User, &key.game, &entry.Rating, &entry.JoinedAt, &key.seats, &key.options, &options); err != nil {
			rows.Close()
			return nil, err
		}
		if _, ok := queues[key]; !ok {
			order = append(order, key)
			tables[key] = Table{Seats: key.seats, Options: options}
		}
		queues[key] = append(queues[key], entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var created []Pairing
	for _, key := range order {
		for _, group := range matchmaking.Groups(queues[key], key.seats, now, wait) {
			item := Pairing{ID: uuid.New(), Game: key.game, Players: group, Table: tables[key]}
			if _, err := tx.Exec(ctx, `UPDATE game.match_queue SET session_id=$1, started=false WHERE game=$2 AND user_id = ANY($3)`, item.ID, key.game, group); err != nil {
				return nil, err
			}
			created = append(created, item)
		}
	}
	return created, tx.Commit(ctx)
}

type queueKey struct {
	game    engine.Key
	seats   int
	options string
}

type Pairing struct {
	ID      uuid.UUID
	Game    engine.Key
	Players []uuid.UUID
	Table   Table
}

func (p Pairing) Plain() bool {
	return len(p.Players) == DefaultSeats && len(p.Table.Options) == 0
}

func (s *Store) Pending(ctx context.Context) ([]Pairing, error) {
	rows, err := s.pool.Query(ctx, `SELECT session_id, game, array_agg(user_id ORDER BY joined_at, user_id), max(seats), (array_agg(options))[1]
		FROM game.match_queue WHERE session_id IS NOT NULL AND NOT started GROUP BY session_id, game`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []Pairing
	for rows.Next() {
		var item Pairing
		var seats int
		var options []byte
		if err := rows.Scan(&item.ID, &item.Game, &item.Players, &seats, &options); err != nil {
			return nil, err
		}
		if len(item.Players) != seats {
			continue
		}
		item.Table = Table{Seats: seats, Options: options}
		pending = append(pending, item)
	}
	return pending, rows.Err()
}

func (s *Store) Started(ctx context.Context, item Pairing) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE game.match_queue SET started=true WHERE session_id=$1`, item.ID); err != nil {
		return err
	}
	for _, user := range item.Players {
		if _, err := tx.Exec(ctx, `SELECT pg_notify($1,$2)`, matchChannel, user.String()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const (
	OutboxTable   = "tournament.outbox"
	InitialRating = 1200
)

var ErrNotFound = errors.New("store: tournament not found")

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const tournamentColumns = `id, creator_id, name, game, format, capacity, requested_rounds, total_rounds, status, current_round,
	winner_id, created_at, started_at, finished_at, cancelled_at`

func scanTournament(row pgx.Row, extra ...any) (saga.Tournament, error) {
	var t saga.Tournament
	var winner *uuid.UUID
	targets := append([]any{&t.ID, &t.Creator, &t.Spec.Name, &t.Spec.Game, &t.Spec.Format, &t.Spec.Capacity, &t.Spec.Rounds,
		&t.TotalRounds, &t.Status, &t.Round, &winner, &t.CreatedAt, &t.StartedAt, &t.FinishedAt, &t.CancelledAt}, extra...)
	if err := row.Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return saga.Tournament{}, ErrNotFound
		}
		return saga.Tournament{}, err
	}
	if winner != nil {
		t.Winner = *winner
	}
	return t, nil
}

func load(ctx context.Context, db querier, id uuid.UUID, lock bool) (saga.Tournament, error) {
	query := `SELECT ` + tournamentColumns + ` FROM tournament.tournaments WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	t, err := scanTournament(db.QueryRow(ctx, query, id))
	if err != nil {
		return saga.Tournament{}, err
	}
	rows, err := db.Query(ctx, `SELECT user_id, rating, COALESCE(seed, 0), registered_at, returned FROM tournament.registrations
		WHERE tournament_id = $1 ORDER BY registered_at, user_id`, id)
	if err != nil {
		return saga.Tournament{}, err
	}
	if t.Players, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (saga.Player, error) {
		var p saga.Player
		err := row.Scan(&p.User, &p.Rating, &p.Seed, &p.RegisteredAt, &p.Returned)
		return p, err
	}); err != nil {
		return saga.Tournament{}, err
	}
	rows, err = db.Query(ctx, `SELECT number, started_at, finished_at FROM tournament.rounds WHERE tournament_id = $1 ORDER BY number`, id)
	if err != nil {
		return saga.Tournament{}, err
	}
	if t.Rounds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (saga.Round, error) {
		var r saga.Round
		err := row.Scan(&r.Number, &r.StartedAt, &r.FinishedAt)
		return r, err
	}); err != nil {
		return saga.Tournament{}, err
	}
	rows, err = db.Query(ctx, `SELECT round, board, session_id, first_id, second_id, result, winner_id, forfeit, finished_at
		FROM tournament.matches WHERE tournament_id = $1 ORDER BY round, board`, id)
	if err != nil {
		return saga.Tournament{}, err
	}
	t.Matches, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (saga.Match, error) {
		var m saga.Match
		var session, second, winner *uuid.UUID
		if err := row.Scan(&m.Round, &m.Board, &session, &m.Seats[0], &second, &m.Result, &winner, &m.Forfeit, &m.FinishedAt); err != nil {
			return m, err
		}
		if session != nil {
			m.Session = *session
		}
		if second != nil {
			m.Seats[1] = *second
		}
		if winner != nil {
			m.Winner = *winner
		}
		return m, nil
	})
	if err != nil {
		return saga.Tournament{}, err
	}
	return t, nil
}

func nullable(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (s *Postgres) Create(ctx context.Context, t saga.Tournament, request uuid.UUID) (saga.Tournament, bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO tournament.tournaments (id, creator_id, request_id, name, game, format, capacity, requested_rounds,
			total_rounds, status, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING`,
		t.ID, t.Creator, request, t.Spec.Name, t.Spec.Game, t.Spec.Format, t.Spec.Capacity, t.Spec.Rounds, t.TotalRounds, t.Status, t.CreatedAt)
	if err != nil {
		return saga.Tournament{}, false, fmt.Errorf("store: create %s: %w", t.ID, err)
	}
	stored, err := s.Get(ctx, t.ID)
	return stored, tag.RowsAffected() == 1, err
}

func (s *Postgres) Get(ctx context.Context, id uuid.UUID) (saga.Tournament, error) {
	var out saga.Tournament
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		out, err = load(ctx, tx, id, false)
		return err
	})
	return out, err
}

type Tx struct {
	tx pgx.Tx
}

type Mutation func(ctx context.Context, tx *Tx, t saga.Tournament) (saga.Tournament, error)

func (s *Postgres) Update(ctx context.Context, id uuid.UUID, mutate Mutation) (saga.Tournament, error) {
	var out saga.Tournament
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		before, err := load(ctx, tx, id, true)
		if err != nil {
			return err
		}
		after, err := mutate(ctx, &Tx{tx: tx}, before)
		if err != nil {
			return err
		}
		if err := save(ctx, tx, before, after); err != nil {
			return err
		}
		out = after
		return nil
	})
	if err != nil {
		return saga.Tournament{}, err
	}
	return out, nil
}

func sameTime(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func save(ctx context.Context, tx pgx.Tx, before, after saga.Tournament) error {
	batch := &pgx.Batch{}
	if before.Status != after.Status || before.Round != after.Round || before.TotalRounds != after.TotalRounds || before.Winner != after.Winner ||
		!sameTime(before.StartedAt, after.StartedAt) || !sameTime(before.FinishedAt, after.FinishedAt) || !sameTime(before.CancelledAt, after.CancelledAt) {
		batch.Queue(`UPDATE tournament.tournaments SET total_rounds=$2, status=$3, current_round=$4, winner_id=$5, started_at=$6, finished_at=$7,
				cancelled_at=$8, updated_at=clock_timestamp() WHERE id=$1`,
			after.ID, after.TotalRounds, after.Status, after.Round, nullable(after.Winner), after.StartedAt, after.FinishedAt, after.CancelledAt)
	}
	players := make(map[uuid.UUID]saga.Player, len(before.Players))
	for _, p := range before.Players {
		players[p.User] = p
	}
	for _, p := range after.Players {
		if old, ok := players[p.User]; ok && old.Rating == p.Rating && old.Seed == p.Seed && old.Returned == p.Returned {
			delete(players, p.User)
			continue
		}
		delete(players, p.User)
		var seed *int
		if p.Seed > 0 {
			seed = &p.Seed
		}
		batch.Queue(`INSERT INTO tournament.registrations (tournament_id, user_id, rating, seed, registered_at, returned) VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (tournament_id, user_id) DO UPDATE SET rating=EXCLUDED.rating, seed=EXCLUDED.seed, returned=EXCLUDED.returned`,
			after.ID, p.User, p.Rating, seed, p.RegisteredAt, p.Returned)
	}
	for user := range players {
		batch.Queue(`DELETE FROM tournament.registrations WHERE tournament_id=$1 AND user_id=$2`, after.ID, user)
	}
	rounds := make(map[int]saga.Round, len(before.Rounds))
	for _, r := range before.Rounds {
		rounds[r.Number] = r
	}
	for _, r := range after.Rounds {
		if old, ok := rounds[r.Number]; ok && sameTime(old.FinishedAt, r.FinishedAt) {
			continue
		}
		batch.Queue(`INSERT INTO tournament.rounds (tournament_id, number, started_at, finished_at) VALUES ($1,$2,$3,$4)
			ON CONFLICT (tournament_id, number) DO UPDATE SET finished_at=EXCLUDED.finished_at`, after.ID, r.Number, r.StartedAt, r.FinishedAt)
	}
	type key struct{ round, board int }
	matches := make(map[key]saga.Match, len(before.Matches))
	for _, m := range before.Matches {
		matches[key{m.Round, m.Board}] = m
	}
	for _, m := range after.Matches {
		if old, ok := matches[key{m.Round, m.Board}]; ok && old.Result == m.Result && old.Winner == m.Winner && old.Forfeit == m.Forfeit &&
			sameTime(old.FinishedAt, m.FinishedAt) {
			continue
		}
		batch.Queue(`INSERT INTO tournament.matches (tournament_id, round, board, session_id, first_id, second_id, result, winner_id, forfeit, finished_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			ON CONFLICT (tournament_id, round, board) DO UPDATE SET result=EXCLUDED.result, winner_id=EXCLUDED.winner_id,
				forfeit=EXCLUDED.forfeit, finished_at=EXCLUDED.finished_at`,
			after.ID, m.Round, m.Board, nullable(m.Session), m.Seats[0], nullable(m.Seats[1]), m.Result, nullable(m.Winner), m.Forfeit, m.FinishedAt)
	}
	if batch.Len() == 0 {
		return nil
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("store: save %s: %w", after.ID, err)
	}
	return nil
}

func (tx *Tx) Publish(ctx context.Context, events ...outbox.Event) error {
	return outbox.Write(ctx, tx.tx, OutboxTable, events...)
}

func (tx *Tx) Claim(ctx context.Context, event, session uuid.UUID) (bool, error) {
	tag, err := tx.tx.Exec(ctx, `INSERT INTO tournament.processed_events (event_id, session_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, event, session)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (tx *Tx) Schedule(ctx context.Context, tournament uuid.UUID, sessions []uuid.UUID, due time.Time) error {
	if len(sessions) == 0 {
		return nil
	}
	_, err := tx.tx.Exec(ctx, `INSERT INTO tournament.deadlines (session_id, tournament_id, due_at)
		SELECT s, $1, $3 FROM unnest($2::uuid[]) AS s ON CONFLICT (session_id) DO NOTHING`, tournament, sessions, due)
	return err
}

type Deadline struct {
	Session    uuid.UUID
	Tournament uuid.UUID
	Due        time.Time
}

func (tx *Tx) Deadline(ctx context.Context, session uuid.UUID) (Deadline, bool, error) {
	d := Deadline{Session: session}
	err := tx.tx.QueryRow(ctx, `SELECT tournament_id, due_at FROM tournament.deadlines WHERE session_id = $1 AND fired_at IS NULL FOR UPDATE`, session).
		Scan(&d.Tournament, &d.Due)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deadline{}, false, nil
	}
	return d, err == nil, err
}

func (tx *Tx) Fire(ctx context.Context, session uuid.UUID, forfeited bool, at time.Time) error {
	_, err := tx.tx.Exec(ctx, `UPDATE tournament.deadlines SET fired_at = $2, forfeited = $3 WHERE session_id = $1`, session, at, forfeited)
	return err
}

func (s *Postgres) Due(ctx context.Context, now time.Time, limit int) ([]Deadline, error) {
	rows, err := s.pool.Query(ctx, `SELECT session_id, tournament_id, due_at FROM tournament.deadlines
		WHERE fired_at IS NULL AND due_at <= $1 ORDER BY due_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Deadline, error) {
		var d Deadline
		err := row.Scan(&d.Session, &d.Tournament, &d.Due)
		return d, err
	})
}

func (s *Postgres) Stalled(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id FROM tournament.tournaments t
		WHERE t.status = 'seeding' OR t.status = 'running' AND NOT EXISTS (
			SELECT 1 FROM tournament.matches m WHERE m.tournament_id = t.id AND m.round = t.current_round AND m.result = 'pending')
		ORDER BY t.updated_at`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

func (s *Postgres) Active(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tournament.tournaments WHERE status IN ('seeding', 'running')`).Scan(&n)
	return n, err
}

func (s *Postgres) TournamentOf(ctx context.Context, session uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT tournament_id FROM tournament.matches WHERE session_id = $1`, session).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

type Summary struct {
	Tournament saga.Tournament
	Players    int
}

func (s *Postgres) List(ctx context.Context, status saga.Status, limit int) ([]Summary, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+tournamentColumns+`,
			(SELECT count(*) FROM tournament.registrations r WHERE r.tournament_id = t.id AND NOT r.returned)
		FROM tournament.tournaments t WHERE $1 = '' OR status = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, string(status), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Summary, error) {
		var out Summary
		t, err := scanTournament(row, &out.Players)
		out.Tournament = t
		return out, err
	})
}

func (s *Postgres) Rating(ctx context.Context, game string, user uuid.UUID) (int, error) {
	var rating int
	err := s.pool.QueryRow(ctx, `SELECT rating FROM tournament.ratings WHERE game = $1 AND user_id = $2`, game, user).Scan(&rating)
	if errors.Is(err, pgx.ErrNoRows) {
		return InitialRating, nil
	}
	return rating, err
}

func (s *Postgres) SetRatings(ctx context.Context, game string, at time.Time, ratings map[uuid.UUID]int) error {
	batch := &pgx.Batch{}
	for user, rating := range ratings {
		batch.Queue(`INSERT INTO tournament.ratings (game, user_id, rating, changed_at) VALUES ($1,$2,$3,$4)
			ON CONFLICT (game, user_id) DO UPDATE SET rating = EXCLUDED.rating, changed_at = EXCLUDED.changed_at
			WHERE tournament.ratings.changed_at <= EXCLUDED.changed_at`, game, user, rating, at)
	}
	if batch.Len() == 0 {
		return nil
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

func (s *Postgres) PruneOutbox(ctx context.Context, every, olderThan time.Duration, log *slog.Logger) {
	for {
		removed, err := outbox.Cleanup(ctx, s.pool, OutboxTable, olderThan)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.WarnContext(ctx, "outbox cleanup failed", "error", err)
		case removed > 0:
			log.InfoContext(ctx, "outbox rows removed", "count", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
)

type Status string

const (
	Pending Status = "pending"
	Ready   Status = "ready"
	Failed  Status = "failed"
)

var ErrNotFound = errors.New("store: no analysis for the session")

type Job struct {
	Event      uuid.UUID
	Session    uuid.UUID
	Moves      int
	Depth      int
	Engine     string
	FinishedAt time.Time
}

type Outcome struct {
	Positions []review.Position
	Duration  time.Duration
	Err       string
}

func (o Outcome) Status() Status {
	if o.Err != "" {
		return Failed
	}
	return Ready
}

type Report struct {
	Session     uuid.UUID
	Event       uuid.UUID
	Status      Status
	Depth       int
	Engine      string
	Moves       int
	FinishedAt  time.Time
	StartedAt   time.Time
	CompletedAt *time.Time
	Duration    time.Duration
	Err         string
	Positions   []review.Position
}

type Postgres struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (s *Postgres) Processed(ctx context.Context, event uuid.UUID) (bool, error) {
	var seen bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM analysis.processed_events WHERE event_id = $1)`, event).Scan(&seen); err != nil {
		return false, fmt.Errorf("store: processed %s: %w", event, err)
	}
	return seen, nil
}

func (s *Postgres) Start(ctx context.Context, job Job) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO analysis.reports (session_id, event_id, status, depth, engine, moves, game_finished_at)
		VALUES ($1, $2, 'pending', $3, $4, $5, $6)
		ON CONFLICT (session_id) DO UPDATE SET event_id = EXCLUDED.event_id, depth = EXCLUDED.depth, engine = EXCLUDED.engine,
			moves = EXCLUDED.moves, game_finished_at = EXCLUDED.game_finished_at, started_at = clock_timestamp()
		WHERE analysis.reports.status = 'pending'`,
		job.Session, job.Event, job.Depth, job.Engine, job.Moves, job.FinishedAt)
	if err != nil {
		return false, fmt.Errorf("store: start %s: %w", job.Session, err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Postgres) Finish(ctx context.Context, job Job, outcome Outcome) (bool, error) {
	finished := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO analysis.processed_events (event_id, session_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, job.Event, job.Session)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		var errText *string
		if outcome.Err != "" {
			errText = &outcome.Err
		}
		tag, err = tx.Exec(ctx, `UPDATE analysis.reports SET status = $2, error = $3, completed_at = clock_timestamp(), duration_ms = $4
			WHERE session_id = $1 AND status = 'pending'`, job.Session, string(outcome.Status()), errText, outcome.Duration.Milliseconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		rows := make([][]any, len(outcome.Positions))
		for i, p := range outcome.Positions {
			rows[i] = []any{job.Session, p.Ply, p.Centipawns, p.Mate, optional(p.BestMove), optional(p.Played), p.Loss, string(p.Class)}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"analysis", "positions"},
			[]string{"session_id", "ply", "eval_cp", "eval_mate", "best_move", "played_move", "loss", "class"}, pgx.CopyFromRows(rows)); err != nil {
			return err
		}
		finished = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: finish %s: %w", job.Session, err)
	}
	return finished, nil
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (s *Postgres) Get(ctx context.Context, session uuid.UUID) (Report, error) {
	var report Report
	var duration *int64
	var errText *string
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT session_id, event_id, status, depth, engine, moves, game_finished_at, started_at, completed_at, duration_ms, error
			FROM analysis.reports WHERE session_id = $1`, session).Scan(&report.Session, &report.Event, &report.Status, &report.Depth, &report.Engine,
			&report.Moves, &report.FinishedAt, &report.StartedAt, &report.CompletedAt, &duration, &errText)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT ply, eval_cp, eval_mate, coalesce(best_move, ''), coalesce(played_move, ''), loss, class
			FROM analysis.positions WHERE session_id = $1 ORDER BY ply`, session)
		if err != nil {
			return err
		}
		report.Positions, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (review.Position, error) {
			var p review.Position
			var class string
			err := row.Scan(&p.Ply, &p.Centipawns, &p.Mate, &p.BestMove, &p.Played, &p.Loss, &class)
			p.Class = review.Class(class)
			return p, err
		})
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return Report{}, err
	}
	if err != nil {
		return Report{}, fmt.Errorf("store: get %s: %w", session, err)
	}
	if duration != nil {
		report.Duration = time.Duration(*duration) * time.Millisecond
	}
	if errText != nil {
		report.Err = *errText
	}
	return report, nil
}

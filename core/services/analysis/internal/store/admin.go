package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type ReportFilter struct {
	Status Status
	Cursor string
	Limit  int
}

var ErrInvalidCursor = errors.New("store: invalid cursor")

type reportCursor struct {
	At      time.Time `json:"t"`
	Session uuid.UUID `json:"s"`
}

func (s Status) Valid() bool {
	switch s {
	case Pending, Ready, Failed:
		return true
	default:
		return false
	}
}

func (s *Postgres) List(ctx context.Context, f ReportFilter) ([]Report, string, error) {
	var (
		afterAt      *time.Time
		afterSession *uuid.UUID
	)
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c reportCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", ErrInvalidCursor
		}
		afterAt, afterSession = &c.At, &c.Session
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_id, event_id, status, depth, engine, moves, game_finished_at, started_at, completed_at, duration_ms, error
		FROM analysis.reports
		WHERE ($1::text = '' OR status = $1::text)
			AND ($2::timestamptz IS NULL OR (started_at, session_id) < ($2::timestamptz, $3::uuid))
		ORDER BY started_at DESC, session_id DESC
		LIMIT $4`, string(f.Status), afterAt, afterSession, f.Limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("store: list reports: %w", err)
	}
	defer rows.Close()
	out := []Report{}
	for rows.Next() {
		var (
			r        Report
			duration *int64
			errText  *string
		)
		if err := rows.Scan(&r.Session, &r.Event, &r.Status, &r.Depth, &r.Engine, &r.Moves, &r.FinishedAt, &r.StartedAt, &r.CompletedAt, &duration, &errText); err != nil {
			return nil, "", fmt.Errorf("store: scan report: %w", err)
		}
		if duration != nil {
			r.Duration = time.Duration(*duration) * time.Millisecond
		}
		if errText != nil {
			r.Err = *errText
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: list reports: %w", err)
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[f.Limit-1]
		raw, _ := json.Marshal(reportCursor{At: last.StartedAt, Session: last.Session})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, next, nil
}

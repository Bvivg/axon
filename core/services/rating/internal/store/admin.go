package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
)

type Change struct {
	Session        uuid.UUID
	User           uuid.UUID
	Game           string
	RatingBefore   float64
	RatingAfter    float64
	DeviationAfter float64
	FinishedAt     time.Time
}

type ChangeFilter struct {
	Game   string
	User   *uuid.UUID
	Cursor string
	Limit  int
}

var ErrInvalidCursor = errors.New("store: invalid cursor")

type changeCursor struct {
	At      time.Time `json:"t"`
	Session uuid.UUID `json:"s"`
	User    uuid.UUID `json:"u"`
}

func (s *Postgres) Changes(ctx context.Context, f ChangeFilter) ([]Change, string, error) {
	var after *changeCursor
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c changeCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", ErrInvalidCursor
		}
		after = &c
	}
	var (
		afterAt                 *time.Time
		afterSession, afterUser *uuid.UUID
	)
	if after != nil {
		afterAt, afterSession, afterUser = &after.At, &after.Session, &after.User
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_id, user_id, game, rating_before, rating_after, deviation_after, finished_at
		FROM rating.changes
		WHERE ($1 = '' OR game = $1)
			AND ($2::uuid IS NULL OR user_id = $2)
			AND ($3::timestamptz IS NULL OR (finished_at, session_id, user_id) < ($3, $4::uuid, $5::uuid))
		ORDER BY finished_at DESC, session_id DESC, user_id DESC
		LIMIT $6`, f.Game, f.User, afterAt, afterSession, afterUser, f.Limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("store: rating changes: %w", err)
	}
	defer rows.Close()
	var out []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Session, &c.User, &c.Game, &c.RatingBefore, &c.RatingAfter, &c.DeviationAfter, &c.FinishedAt); err != nil {
			return nil, "", fmt.Errorf("store: scan rating change: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("store: rating changes: %w", err)
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[f.Limit-1]
		raw, _ := json.Marshal(changeCursor{At: last.FinishedAt, Session: last.Session, User: last.User})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, next, nil
}

type RatingRow struct {
	Game      string
	Player    ledger.Player
	UpdatedAt time.Time
}

type RatingFilter struct {
	Game   string
	User   *uuid.UUID
	Cursor string
	Limit  int
}

type ratingCursor struct {
	Game   string    `json:"g"`
	Rating float64   `json:"r"`
	User   uuid.UUID `json:"u"`
}

func (s *Postgres) Ratings(ctx context.Context, f RatingFilter) ([]RatingRow, string, error) {
	var (
		afterGame   *string
		afterRating *float64
		afterUser   *uuid.UUID
	)
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c ratingCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return nil, "", ErrInvalidCursor
		}
		afterGame, afterRating, afterUser = &c.Game, &c.Rating, &c.User
	}
	rows, err := s.pool.Query(ctx, `
		SELECT game, `+columns+`, updated_at
		FROM rating.ratings
		WHERE ($1 = '' OR game = $1)
			AND ($2::uuid IS NULL OR user_id = $2)
			AND ($3::text IS NULL OR game > $3
				OR (game = $3 AND (rating < $4::double precision OR (rating = $4 AND user_id > $5::uuid))))
		ORDER BY game, rating DESC, user_id
		LIMIT $6`, f.Game, f.User, afterGame, afterRating, afterUser, f.Limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("store: ratings: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RatingRow, error) {
		var r RatingRow
		p := &r.Player
		err := row.Scan(&r.Game, &p.User, &p.Rating.Rating, &p.Rating.Deviation, &p.Rating.Volatility, &p.Games, &p.Wins, &p.Losses, &p.Draws, &r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return nil, "", fmt.Errorf("store: ratings: %w", err)
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		last := out[f.Limit-1]
		raw, _ := json.Marshal(ratingCursor{Game: last.Game, Rating: last.Player.Rating.Rating, User: last.Player.User})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, next, nil
}

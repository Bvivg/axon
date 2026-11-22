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
	"github.com/bvivg/axon/core/shared/pkg/adminquery"
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
	List   adminquery.Request
}

type ChangePage struct {
	Changes []Change
	Next    string
	Total   *int64
}

var ErrInvalidCursor = errors.New("store: invalid cursor")

type changeCursor struct {
	At      time.Time `json:"t"`
	Session uuid.UUID `json:"s"`
	User    uuid.UUID `json:"u"`
}

const changeOrder = "finished_at DESC, session_id DESC, user_id DESC"

var changeSchema = adminquery.NewSchema(changeOrder, "session_id DESC, user_id DESC",
	adminquery.Field{Name: "user", Column: "user_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "game", Column: "game", Kind: adminquery.String},
	adminquery.Field{Name: "session", Column: "session_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "delta", Column: "round(rating_after - rating_before)", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "rating_before", Column: "round(rating_before)", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "rating_after", Column: "round(rating_after)", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "finished", Column: "finished_at", Kind: adminquery.Time, Sortable: true},
)

func ownerWhere(game string, user *uuid.UUID) adminquery.Where {
	var where adminquery.Where
	if game != "" {
		where.Add("game = ?", game)
	}
	if user != nil {
		where.Add("user_id = ?", *user)
	}
	return where
}

func (s *Postgres) Changes(ctx context.Context, f ChangeFilter) (ChangePage, error) {
	where := ownerWhere(f.Game, f.User)
	listed, err := changeSchema.Prepare(f.List, &where)
	if err != nil {
		return ChangePage{}, err
	}
	var page ChangePage
	order, limit, paging := changeOrder, f.Limit, ""
	if f.List.Paged() {
		var total int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM rating.changes`+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return ChangePage{}, fmt.Errorf("store: count rating changes: %w", err)
		}
		page.Total = &total
		var offset int
		limit, offset = f.List.Page()
		order, paging = listed.Order, where.Paginate(limit, offset)
	} else {
		if f.Cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
			var c changeCursor
			if err != nil || json.Unmarshal(raw, &c) != nil {
				return ChangePage{}, ErrInvalidCursor
			}
			where.Add("(finished_at, session_id, user_id) < (?, ?, ?)", c.At, c.Session, c.User)
		}
		paging = " LIMIT " + where.Arg(limit+1)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_id, user_id, game, rating_before, rating_after, deviation_after, finished_at
		FROM rating.changes`+where.SQL()+`
		ORDER BY `+order+paging, where.Args()...)
	if err != nil {
		return ChangePage{}, fmt.Errorf("store: rating changes: %w", err)
	}
	page.Changes, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Change, error) {
		var c Change
		err := row.Scan(&c.Session, &c.User, &c.Game, &c.RatingBefore, &c.RatingAfter, &c.DeviationAfter, &c.FinishedAt)
		return c, err
	})
	if err != nil {
		return ChangePage{}, fmt.Errorf("store: rating changes: %w", err)
	}
	if !f.List.Paged() && len(page.Changes) > limit {
		page.Changes = page.Changes[:limit]
		last := page.Changes[limit-1]
		raw, _ := json.Marshal(changeCursor{At: last.FinishedAt, Session: last.Session, User: last.User})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
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
	List   adminquery.Request
}

type RatingPage struct {
	Ratings []RatingRow
	Next    string
	Total   *int64
}

type ratingCursor struct {
	Game   string    `json:"g"`
	Rating float64   `json:"r"`
	User   uuid.UUID `json:"u"`
}

const ratingOrder = "game, rating DESC, user_id"

var ratingSchema = adminquery.NewSchema(ratingOrder, "game, user_id",
	adminquery.Field{Name: "user", Column: "user_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "game", Column: "game", Kind: adminquery.String},
	adminquery.Field{Name: "rating", Column: "round(rating)", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "games", Column: "games", Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "updated", Column: "updated_at", Kind: adminquery.Time, Sortable: true},
)

func (s *Postgres) Ratings(ctx context.Context, f RatingFilter) (RatingPage, error) {
	where := ownerWhere(f.Game, f.User)
	listed, err := ratingSchema.Prepare(f.List, &where)
	if err != nil {
		return RatingPage{}, err
	}
	var page RatingPage
	order, limit, paging := ratingOrder, f.Limit, ""
	if f.List.Paged() {
		var total int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM rating.ratings`+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return RatingPage{}, fmt.Errorf("store: count ratings: %w", err)
		}
		page.Total = &total
		var offset int
		limit, offset = f.List.Page()
		order, paging = listed.Order, where.Paginate(limit, offset)
	} else {
		if f.Cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
			var c ratingCursor
			if err != nil || json.Unmarshal(raw, &c) != nil {
				return RatingPage{}, ErrInvalidCursor
			}
			where.Add("(game > ? OR (game = ? AND (rating < ? OR (rating = ? AND user_id > ?))))", c.Game, c.Game, c.Rating, c.Rating, c.User)
		}
		paging = " LIMIT " + where.Arg(limit+1)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT game, `+columns+`, updated_at
		FROM rating.ratings`+where.SQL()+`
		ORDER BY `+order+paging, where.Args()...)
	if err != nil {
		return RatingPage{}, fmt.Errorf("store: ratings: %w", err)
	}
	page.Ratings, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (RatingRow, error) {
		var r RatingRow
		p := &r.Player
		err := row.Scan(&r.Game, &p.User, &p.Rating.Rating, &p.Rating.Deviation, &p.Rating.Volatility, &p.Games, &p.Wins, &p.Losses, &p.Draws, &r.UpdatedAt)
		return r, err
	})
	if err != nil {
		return RatingPage{}, fmt.Errorf("store: ratings: %w", err)
	}
	if !f.List.Paged() && len(page.Ratings) > limit {
		page.Ratings = page.Ratings[:limit]
		last := page.Ratings[limit-1]
		raw, _ := json.Marshal(ratingCursor{Game: last.Game, Rating: last.Player.Rating.Rating, User: last.Player.User})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

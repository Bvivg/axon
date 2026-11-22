package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

type ReportFilter struct {
	Status Status
	Cursor string
	Limit  int
	List   adminquery.Request
}

type ReportPage struct {
	Reports []Report
	Next    string
	Total   *int64
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

const reportOrder = "started_at DESC, session_id DESC"

var reportSchema = adminquery.NewSchema(reportOrder, "session_id DESC",
	adminquery.Field{Name: "session", Column: "session_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "status", Kind: adminquery.Enum, Values: map[string]string{
		string(Pending): "status = 'pending'",
		string(Ready):   "status = 'ready'",
		string(Failed):  "status = 'failed'",
	}},
	adminquery.Field{Name: "started", Column: "started_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "finished", Column: "completed_at", Kind: adminquery.Time, Sortable: true},
)

func (s *Postgres) List(ctx context.Context, f ReportFilter) (ReportPage, error) {
	var where adminquery.Where
	if f.Status != "" {
		where.Add("status = ?", string(f.Status))
	}
	listed, err := reportSchema.Prepare(f.List, &where)
	if err != nil {
		return ReportPage{}, err
	}
	var page ReportPage
	order, limit, paging := reportOrder, f.Limit, ""
	if f.List.Paged() {
		var total int64
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM analysis.reports`+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return ReportPage{}, fmt.Errorf("store: count reports: %w", err)
		}
		page.Total = &total
		var offset int
		limit, offset = f.List.Page()
		order, paging = listed.Order, where.Paginate(limit, offset)
	} else {
		if f.Cursor != "" {
			raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
			var c reportCursor
			if err != nil || json.Unmarshal(raw, &c) != nil {
				return ReportPage{}, ErrInvalidCursor
			}
			where.Add("(started_at, session_id) < (?, ?)", c.At, c.Session)
		}
		paging = " LIMIT " + where.Arg(limit+1)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT session_id, event_id, status, depth, engine, moves, game_finished_at, started_at, completed_at, duration_ms, error
		FROM analysis.reports`+where.SQL()+`
		ORDER BY `+order+paging, where.Args()...)
	if err != nil {
		return ReportPage{}, fmt.Errorf("store: list reports: %w", err)
	}
	defer rows.Close()
	page.Reports = []Report{}
	for rows.Next() {
		var (
			r        Report
			duration *int64
			errText  *string
		)
		if err := rows.Scan(&r.Session, &r.Event, &r.Status, &r.Depth, &r.Engine, &r.Moves, &r.FinishedAt, &r.StartedAt, &r.CompletedAt, &duration, &errText); err != nil {
			return ReportPage{}, fmt.Errorf("store: scan report: %w", err)
		}
		if duration != nil {
			r.Duration = time.Duration(*duration) * time.Millisecond
		}
		if errText != nil {
			r.Err = *errText
		}
		page.Reports = append(page.Reports, r)
	}
	if err := rows.Err(); err != nil {
		return ReportPage{}, fmt.Errorf("store: list reports: %w", err)
	}
	if !f.List.Paged() && len(page.Reports) > limit {
		page.Reports = page.Reports[:limit]
		last := page.Reports[limit-1]
		raw, _ := json.Marshal(reportCursor{At: last.StartedAt, Session: last.Session})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

const (
	callEnded        = `(c.document->>'ended_at')::timestamptz`
	callDuration     = `floor(extract(epoch FROM coalesce(` + callEnded + `, now()) - (c.document->>'started_at')::timestamptz))::bigint`
	callParticipants = `CASE WHEN jsonb_typeof(c.document->'participants') = 'array' THEN jsonb_array_length(c.document->'participants') ELSE 0 END`
)

var callSchema = adminquery.NewSchema(`c.created_at DESC, c.id DESC`, `c.id DESC`,
	adminquery.Field{Name: "id", Column: "c.id", Kind: adminquery.UUID},
	adminquery.Field{Name: "state", Column: "c.state", Kind: adminquery.Enum, Values: equalsOneOf("c.state",
		calls.Ringing, calls.Connecting, calls.Active, calls.Ended)},
	adminquery.Field{Name: "active", Column: "(c.state <> 'ended')", Kind: adminquery.Bool},
	adminquery.Field{Name: "kind", Kind: adminquery.Enum, Values: map[string]string{
		"direct": "c.document->>'kind' IS DISTINCT FROM 'group'",
		"group":  "c.document->>'kind' = 'group'",
	}},
	adminquery.Field{Name: "mode", Kind: adminquery.Enum, Values: equalsOneOf("c.document->>'mode'", "audio", "video")},
	adminquery.Field{Name: "started", Column: "c.created_at", Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "ended", Column: callEnded, Kind: adminquery.Time, Sortable: true},
	adminquery.Field{Name: "duration", Column: callDuration, Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "participants", Column: callParticipants, Kind: adminquery.Int, Sortable: true},
	adminquery.Field{Name: "room", Column: "c.room_id", Kind: adminquery.UUID},
	adminquery.Field{Name: "user", Kind: adminquery.Custom,
		Build: matchesID("user", `(c.caller_id = ? OR EXISTS (SELECT 1 FROM call_participants fp WHERE fp.call_id = c.id AND fp.user_id = ?))`)},
)

func (r *Repository) AdminCalls(ctx context.Context, f calls.AdminFilter) (calls.AdminPage, error) {
	page := calls.AdminPage{Calls: []calls.Call{}}
	if err := r.q.QueryRow(ctx, `SELECT count(*) FROM calls WHERE state <> 'ended'`).Scan(&page.ActiveTotal); err != nil {
		return calls.AdminPage{}, fmt.Errorf("repository: count active calls: %w", err)
	}
	var where adminquery.Where
	if f.ActiveOnly {
		where.Add("c.state <> 'ended'")
	}
	if f.User != nil {
		where.Add("EXISTS (SELECT 1 FROM call_participants p WHERE p.call_id = c.id AND p.user_id = ?)", *f.User)
	}
	listed, err := callSchema.Prepare(f.List, &where)
	if err != nil {
		return calls.AdminPage{}, fmt.Errorf("repository: admin calls: %w", err)
	}

	if f.List.Paged() {
		var total int64
		if err := r.q.QueryRow(ctx, `SELECT count(*) FROM calls c`+where.SQL(), where.Args()...).Scan(&total); err != nil {
			return calls.AdminPage{}, fmt.Errorf("repository: count admin calls: %w", err)
		}
		limit, offset := f.List.Page()
		paging := where.Paginate(limit, offset)
		found, err := r.adminCalls(ctx, `SELECT c.document FROM calls c`+where.SQL()+` ORDER BY `+listed.Order+paging, where.Args())
		if err != nil {
			return calls.AdminPage{}, err
		}
		page.Calls, page.Total = found, &total
		return page, nil
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c roomCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return calls.AdminPage{}, calls.ErrInvalid
		}
		where.Add("(c.created_at, c.id) < (?, ?)", c.At, c.ID)
	}
	limit := where.Arg(f.Limit + 1)
	found, err := r.adminCalls(ctx, `SELECT c.document FROM calls c`+where.SQL()+` ORDER BY c.created_at DESC, c.id DESC LIMIT `+limit, where.Args())
	if err != nil {
		return calls.AdminPage{}, err
	}
	page.Calls = found
	if len(page.Calls) > f.Limit {
		page.Calls = page.Calls[:f.Limit]
		last := page.Calls[f.Limit-1]
		raw, _ := json.Marshal(roomCursor{At: last.CreatedAt, ID: last.ID})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

func (r *Repository) adminCalls(ctx context.Context, query string, args []any) ([]calls.Call, error) {
	rows, err := r.q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("repository: admin calls: %w", err)
	}
	defer rows.Close()
	out := []calls.Call{}
	for rows.Next() {
		var raw []byte
		var call calls.Call
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("repository: scan admin call: %w", err)
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, fmt.Errorf("repository: decode admin call: %w", err)
		}
		out = append(out, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: admin calls: %w", err)
	}
	return out, nil
}

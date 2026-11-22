package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func (r *Repository) AdminCalls(ctx context.Context, f calls.AdminFilter) (calls.AdminPage, error) {
	page := calls.AdminPage{Calls: []calls.Call{}}
	if err := r.q.QueryRow(ctx, `SELECT count(*) FROM calls WHERE state <> 'ended'`).Scan(&page.ActiveTotal); err != nil {
		return calls.AdminPage{}, fmt.Errorf("repository: count active calls: %w", err)
	}
	var cursor *roomCursor
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		var c roomCursor
		if err != nil || json.Unmarshal(raw, &c) != nil {
			return calls.AdminPage{}, calls.ErrInvalid
		}
		cursor = &c
	}
	var (
		at   any
		id   any
		user any
	)
	if cursor != nil {
		at, id = cursor.At, cursor.ID
	}
	if f.User != nil {
		user = *f.User
	}
	rows, err := r.q.Query(ctx, `
		SELECT c.document FROM calls c
		WHERE (NOT $1::bool OR c.state <> 'ended')
			AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM call_participants p WHERE p.call_id = c.id AND p.user_id = $2))
			AND ($3::timestamptz IS NULL OR (c.created_at, c.id) < ($3, $4::uuid))
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $5`, f.ActiveOnly, user, at, id, f.Limit+1)
	if err != nil {
		return calls.AdminPage{}, fmt.Errorf("repository: admin calls: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var call calls.Call
		if err := rows.Scan(&raw); err != nil {
			return calls.AdminPage{}, fmt.Errorf("repository: scan admin call: %w", err)
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return calls.AdminPage{}, fmt.Errorf("repository: decode admin call: %w", err)
		}
		page.Calls = append(page.Calls, call)
	}
	if err := rows.Err(); err != nil {
		return calls.AdminPage{}, fmt.Errorf("repository: admin calls: %w", err)
	}
	if len(page.Calls) > f.Limit {
		page.Calls = page.Calls[:f.Limit]
		last := page.Calls[f.Limit-1]
		raw, _ := json.Marshal(roomCursor{At: last.CreatedAt, ID: last.ID})
		page.Next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}

package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

func (r *Repository) SearchUsers(ctx context.Context, query string, excludeUserID uuid.UUID, limit int, after *domain.UserSearchCursor) ([]domain.UserSearchResult, error) {
	const stmt = `
		WITH matches AS (
		SELECT ` + userColumns + `,
			CASE
				WHEN $1 = '' OR email = lower($1) OR lower(display_name) = lower($1) OR username = $4 THEN 0
				WHEN display_name ILIKE $6 OR nickname ILIKE $6 OR ($4 <> '' AND username LIKE $7) THEN 1
				ELSE 2
			END AS search_rank,
			lower(display_name) COLLATE "C" AS search_name
		FROM users
		WHERE id != $2 AND (
			$1 = '' OR email = lower($1)
			OR display_name ILIKE $3
			OR nickname ILIKE $3
			OR ($4 <> '' AND username LIKE $5)
		))
		SELECT ` + userColumns + `, search_rank, search_name FROM matches
		WHERE $9::uuid IS NULL OR (search_rank, search_name, username COLLATE "C", id) >
			($10::int, $11::text COLLATE "C", $12::text COLLATE "C", $9::uuid)
		ORDER BY search_rank, search_name, username COLLATE "C", id
		LIMIT $8`

	username := strings.ToLower(strings.TrimLeft(query, "@"))
	var afterID any
	var cursor domain.UserSearchCursor
	if after != nil {
		cursor = *after
		afterID = cursor.ID
	}
	rows, err := r.q.Query(ctx, stmt,
		query, excludeUserID,
		postgres.Containing(query), username, postgres.Containing(username),
		postgres.StartingWith(query), postgres.StartingWith(username),
		limit, afterID, cursor.Rank, cursor.Name, cursor.Username,
	)
	if err != nil {
		return nil, fmt.Errorf("repository: search users: %w", err)
	}
	defer rows.Close()

	var users []domain.UserSearchResult
	for rows.Next() {
		var result domain.UserSearchResult
		result.User, err = scanUser(rows, &result.Cursor.Rank, &result.Cursor.Name)
		if err != nil {
			return nil, fmt.Errorf("repository: search users: %w", err)
		}
		result.Cursor.ID = result.ID
		result.Cursor.Username = result.Username
		users = append(users, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repository: search users: %w", err)
	}
	return users, nil
}

func (r *Repository) UsersByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	const stmt = `SELECT ` + userColumns + ` FROM users WHERE id = ANY($1) ORDER BY id`

	rows, err := r.q.Query(ctx, stmt, ids)
	if err != nil {
		return nil, fmt.Errorf("repository: users by ids: %w", err)
	}
	defer rows.Close()

	users, err := scanUsers(rows)
	if err != nil {
		return nil, fmt.Errorf("repository: users by ids: %w", err)
	}
	return users, nil
}

func scanUsers(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.User, error) {
	var out []domain.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

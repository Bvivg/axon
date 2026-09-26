package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func (r *Repository) SearchExact(ctx context.Context, query string, excludeUserID uuid.UUID) ([]domain.User, error) {
	const stmt = `
		SELECT ` + userColumns + `
		FROM users
		WHERE (email = lower($1) OR lower(display_name) = lower($1)) AND id != $2
		ORDER BY id`

	rows, err := r.q.Query(ctx, stmt, query, excludeUserID)
	if err != nil {
		return nil, fmt.Errorf("repository: search exact: %w", err)
	}
	defer rows.Close()

	users, err := scanUsers(rows)
	if err != nil {
		return nil, fmt.Errorf("repository: search exact: %w", err)
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

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	q querier

	pool *postgres.Pool
}

func New(pool *postgres.Pool) *Repository {
	return &Repository{q: pool, pool: pool}
}

var ErrNestedTransaction = errors.New("repository: already in a transaction")

func (r *Repository) InTx(ctx context.Context, fn func(*Repository) error) error {
	if r.pool == nil {
		return ErrNestedTransaction
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(&Repository{q: tx}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repository: commit transaction: %w", err)
	}
	return nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

func noRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

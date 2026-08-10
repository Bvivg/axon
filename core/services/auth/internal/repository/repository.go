// Package repository persists the auth domain in Postgres.
//
// Every method translates database failures into domain errors before they
// leave: no pgx or pgconn type escapes this package, so the service layer never
// learns what storage is underneath.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/bvivg/axon/core/shared/pkg/postgres"
)

// querier is the subset of pgx both a pool and a transaction satisfy. Every
// query in this package goes through it, which is what lets the same method
// body run inside or outside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository reads and writes the auth schema.
type Repository struct {
	// q is where queries go: the pool normally, a transaction inside InTx.
	q querier

	// pool is kept separately because only a pool can begin a transaction.
	// Inside InTx it is nil, which makes nesting a compile-time-free but
	// explicit runtime error rather than a silent second transaction.
	pool *postgres.Pool
}

// New returns a Repository backed by pool.
func New(pool *postgres.Pool) *Repository {
	return &Repository{q: pool, pool: pool}
}

// ErrNestedTransaction is returned by InTx when called on a Repository that is
// already inside one.
var ErrNestedTransaction = errors.New("repository: already in a transaction")

// InTx runs fn against a Repository bound to a single transaction, committing
// when fn returns nil and rolling back otherwise.
//
// Registration is the reason this exists: a user row and its credential row
// have to appear together or not at all, or a failure between them leaves an
// account nobody can sign in to and nobody can re-register.
func (r *Repository) InTx(ctx context.Context, fn func(*Repository) error) error {
	if r.pool == nil {
		return ErrNestedTransaction
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repository: begin transaction: %w", err)
	}

	// Rollback after a successful commit is a no-op, so this covers the panic
	// path without needing to know which way fn went.
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

// isUniqueViolation reports whether err is a unique-constraint violation on the
// named constraint. Matching the constraint and not just the SQLSTATE matters:
// a table can have several, and mapping them all to one domain error would tell
// the caller the wrong thing.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// 23505 is unique_violation.
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// noRows reports whether err is pgx's empty-result sentinel.
func noRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

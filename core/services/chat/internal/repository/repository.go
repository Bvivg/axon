// Package repository persists the chat domain in Postgres.
//
// Every method translates database failures into domain errors before they
// leave: no pgx or pgconn type escapes this package, so the layers above never
// learn what storage is underneath.
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

// Repository reads and writes the chat schema.
type Repository struct {
	// q is where queries go: the pool normally, a transaction inside InTx.
	q querier

	// pool is kept separately because only a pool can begin a transaction.
	// Inside InTx it is nil, which makes nesting an explicit error rather than
	// a silent second transaction.
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
// Appending a message is the reason this exists: taking the room's next
// position and writing the message that occupies it have to be one atomic act,
// or a crash in between leaves a gap that a reconnecting client waits for
// forever.
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

// noRows reports whether err is pgx's empty-result sentinel.
func noRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

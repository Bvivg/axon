package keyring

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

const keyColumns = `kid, private_key, activates_at, retires_at`

func scanKeys(rows pgx.Rows) ([]Stored, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Stored, error) {
		var s Stored
		var retires *time.Time
		if err := row.Scan(&s.ID, &s.Sealed, &s.ActivatesAt, &retires); err != nil {
			return Stored{}, err
		}
		if retires != nil {
			s.RetiresAt = *retires
		}
		return s, nil
	})
}

func (p *Postgres) Load(ctx context.Context) ([]Stored, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+keyColumns+` FROM signing_keys
		WHERE retires_at IS NULL OR retires_at > clock_timestamp() ORDER BY activates_at`)
	if err != nil {
		return nil, err
	}
	return scanKeys(rows)
}

func (p *Postgres) Update(ctx context.Context, decide func(now time.Time, keys []Stored) (Change, error)) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('auth.signing_keys'))`); err != nil {
			return err
		}
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+keyColumns+` FROM signing_keys
			WHERE retires_at IS NULL OR retires_at > $1 ORDER BY activates_at`, now)
		if err != nil {
			return err
		}
		stored, err := scanKeys(rows)
		if err != nil {
			return err
		}
		change, err := decide(now, stored)
		if err != nil {
			return err
		}
		for id, at := range change.Retire {
			if _, err := tx.Exec(ctx, `UPDATE signing_keys SET retires_at = $2 WHERE kid = $1 AND retires_at IS NULL`, id, at); err != nil {
				return err
			}
		}
		if change.Insert != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO signing_keys (kid, private_key, activates_at) VALUES ($1, $2, $3)`,
				change.Insert.ID, change.Insert.Sealed, change.Insert.ActivatesAt); err != nil {
				return err
			}
		}
		if !change.PurgeBefore.IsZero() {
			if _, err := tx.Exec(ctx, `DELETE FROM signing_keys WHERE retires_at < $1`, change.PurgeBefore); err != nil {
				return err
			}
		}
		return nil
	})
}

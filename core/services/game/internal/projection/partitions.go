package projection

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const partitionRetry = time.Minute

func (m *Postgres) EnsureMovePartitions(ctx context.Context, monthsAhead int) (int, error) {
	var created int
	if err := m.pool.QueryRow(ctx, `SELECT game.ensure_move_partitions($1)`, monthsAhead).Scan(&created); err != nil {
		return 0, fmt.Errorf("projection: ensure move partitions: %w", err)
	}
	return created, nil
}

func (m *Postgres) MaintainMovePartitions(ctx context.Context, monthsAhead int, every time.Duration, log *slog.Logger) {
	for {
		wait := every
		created, err := m.EnsureMovePartitions(ctx, monthsAhead)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			log.WarnContext(ctx, "move partition maintenance failed", "error", err)
			wait = min(every, partitionRetry)
		case created > 0:
			log.InfoContext(ctx, "move partitions created", "count", created, "months_ahead", monthsAhead)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

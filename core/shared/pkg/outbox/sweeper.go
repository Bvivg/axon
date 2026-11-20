package outbox

import (
	"context"
	"log/slog"
	"time"
)

const (
	DefaultSweepInterval = time.Hour
	DefaultRetention     = 24 * time.Hour
	DefaultSweepTimeout  = time.Minute
)

type Sweeper struct {
	DB        Execer
	Table     string
	Interval  time.Duration
	Retention time.Duration
	Timeout   time.Duration
	Logger    *slog.Logger
}

func (s Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(orDefault(s.Interval, DefaultSweepInterval))
	defer ticker.Stop()
	for {
		s.Sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s Sweeper) Sweep(ctx context.Context) {
	attempt, cancel := context.WithTimeout(ctx, orDefault(s.Timeout, DefaultSweepTimeout))
	defer cancel()
	deleted, err := Cleanup(attempt, s.DB, s.Table, orDefault(s.Retention, DefaultRetention))
	switch {
	case s.Logger == nil, ctx.Err() != nil:
	case err != nil:
		s.Logger.WarnContext(ctx, "outbox cleanup failed", "table", s.Table, "error", err)
	case deleted > 0:
		s.Logger.InfoContext(ctx, "outbox cleaned", "table", s.Table, "deleted", deleted)
	}
}

func orDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

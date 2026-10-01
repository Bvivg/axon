package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const (
	sweepBatch    = 100
	removeTimeout = time.Minute
)

type ObjectRemover interface {
	Remove(ctx context.Context, keys []string) map[string]error
}

type OrphanUploads interface {
	SweepOrphanUploads(
		ctx context.Context,
		olderThan time.Time,
		limit int,
		remove func(context.Context, []domain.Upload) ([]uuid.UUID, error),
	) (int, error)
}

type SweeperConfig struct {
	Objects ObjectRemover
	Uploads OrphanUploads
	URLs    URLBuilder

	Interval    time.Duration
	OrphanAfter time.Duration

	Logger *slog.Logger
}

type Sweeper struct {
	objects ObjectRemover
	uploads OrphanUploads
	urls    URLBuilder

	interval    time.Duration
	orphanAfter time.Duration

	log *slog.Logger
	now func() time.Time
}

func NewSweeper(cfg SweeperConfig) (*Sweeper, error) {
	switch {
	case cfg.Objects == nil:
		return nil, errors.New("attachment: an object store is required")
	case cfg.Uploads == nil:
		return nil, errors.New("attachment: an upload store is required")
	case cfg.Interval <= 0:
		return nil, errors.New("attachment: sweep interval must be positive")
	case cfg.OrphanAfter <= 0:
		return nil, errors.New("attachment: orphan age must be positive")
	case cfg.Logger == nil:
		return nil, errors.New("attachment: logger is required")
	}

	return &Sweeper{
		objects:     cfg.Objects,
		uploads:     cfg.Uploads,
		urls:        cfg.URLs,
		interval:    cfg.Interval,
		orphanAfter: cfg.OrphanAfter,
		log:         cfg.Logger,
		now:         time.Now,
	}, nil
}

func (s *Sweeper) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		if swept, err := s.Sweep(ctx); err != nil {
			s.log.ErrorContext(ctx, "could not sweep unsent uploads", "error", err)
		} else if swept > 0 {
			s.log.InfoContext(ctx, "swept unsent uploads", "count", swept)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	cutoff := s.now().Add(-s.orphanAfter)

	total := 0
	for ctx.Err() == nil {
		swept, err := s.uploads.SweepOrphanUploads(ctx, cutoff, sweepBatch, s.remove)
		total += swept
		if err != nil {
			return total, err
		}
		if swept == 0 {
			break
		}
	}
	return total, nil
}

func (s *Sweeper) remove(ctx context.Context, uploads []domain.Upload) ([]uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(ctx, removeTimeout)
	defer cancel()

	keysOf := make(map[uuid.UUID][]string, len(uploads))
	var all []string
	for _, u := range uploads {
		keys := s.ObjectKeys(u.Payload)
		keysOf[u.ID] = keys
		all = append(all, keys...)
	}
	failures := s.objects.Remove(ctx, all)

	var (
		removed []uuid.UUID
		failed  []error
	)
	for _, u := range uploads {
		var errs []error
		for _, key := range keysOf[u.ID] {
			if err, ok := failures[key]; ok {
				errs = append(errs, err)
			}
		}
		if len(errs) > 0 {
			failed = append(failed, fmt.Errorf("attachment: upload %s: %w", u.ID, errors.Join(errs...)))
			continue
		}
		removed = append(removed, u.ID)
	}

	if len(removed) == 0 && len(failed) > 0 {
		return nil, errors.Join(failed...)
	}
	for _, err := range failed {
		s.log.WarnContext(ctx, "could not remove an unsent upload, will retry", "error", err)
	}
	return removed, nil
}

func (s *Sweeper) ObjectKeys(payload json.RawMessage) []string {
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil
	}

	var keys []string
	for _, value := range fields {
		url, ok := value.(string)
		if !ok {
			continue
		}
		if key, ok := s.urls.Key(url); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

package route

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type Loader func(ctx context.Context) (map[int32]string, error)

type Table struct {
	load    Loader
	mu      sync.RWMutex
	owners  map[int32]string
	changed chan struct{}
}

func New(load Loader) *Table {
	return &Table{load: load, owners: map[int32]string{}, changed: make(chan struct{})}
}

func (t *Table) Address(partition int32) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	address, ok := t.owners[partition]
	return address, ok && address != ""
}

func (t *Table) Changed() <-chan struct{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.changed
}

func (t *Table) Refresh(ctx context.Context) error {
	owners, err := t.load(ctx)
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.owners = owners
	close(t.changed)
	t.changed = make(chan struct{})
	t.mu.Unlock()
	return nil
}

func (t *Table) Run(ctx context.Context, config *pgx.ConnConfig, channel string, log *slog.Logger) {
	for ctx.Err() == nil {
		err := t.listen(ctx, config, channel)
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "owner routing updates disconnected", "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (t *Table) listen(ctx context.Context, config *pgx.ConnConfig, channel string) error {
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return fmt.Errorf("listen for owners: %w", err)
	}
	if err := t.Refresh(ctx); err != nil {
		return err
	}
	for {
		wait, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := conn.WaitForNotification(wait)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && wait.Err() == nil {
			return err
		}
		if err := t.Refresh(ctx); err != nil {
			return err
		}
	}
}

package limit

import (
	"context"
	"errors"
	"time"
)

var ErrSaturated = errors.New("limit: no slot became free in time")

type Gate struct {
	slots chan struct{}
	wait  time.Duration
}

func NewGate(size int, wait time.Duration) *Gate {
	return &Gate{slots: make(chan struct{}, max(size, 1)), wait: max(wait, 0)}
}

func (g *Gate) Acquire(ctx context.Context) (func(), error) {
	select {
	case g.slots <- struct{}{}:
		return g.release, nil
	default:
	}

	if g.wait == 0 {
		return nil, ErrSaturated
	}

	timer := time.NewTimer(g.wait)
	defer timer.Stop()

	select {
	case g.slots <- struct{}{}:
		return g.release, nil
	case <-timer.C:
		return nil, ErrSaturated
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *Gate) release() { <-g.slots }

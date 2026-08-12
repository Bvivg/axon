package ws

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

type Backoff struct {
	Initial time.Duration

	Max time.Duration

	Factor float64

	Jitter float64
}

var DefaultBackoff = Backoff{
	Initial: 500 * time.Millisecond,
	Max:     30 * time.Second,
	Factor:  2,
	Jitter:  0.2,
}

var ErrInvalidBackoff = errors.New("invalid backoff")

func (b Backoff) Validate() error {
	switch {
	case b.Initial <= 0:
		return fmt.Errorf("%w: initial delay must be positive", ErrInvalidBackoff)
	case b.Max < b.Initial:
		return fmt.Errorf("%w: max delay is below the initial delay", ErrInvalidBackoff)
	case b.Factor < 1:
		return fmt.Errorf("%w: factor below 1 would shrink the delay", ErrInvalidBackoff)
	case b.Jitter < 0 || b.Jitter > 1:
		return fmt.Errorf("%w: jitter must be between 0 and 1", ErrInvalidBackoff)
	}
	return nil
}

func (b Backoff) Delay(attempt int) time.Duration {

	return b.delay(attempt, rand.Float64()) //nolint:gosec
}

func (b Backoff) delay(attempt int, random float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	d := float64(b.Initial) * math.Pow(b.Factor, float64(attempt))
	if d > float64(b.Max) {
		d = float64(b.Max)
	}

	d -= d * b.Jitter * random

	return time.Duration(d)
}

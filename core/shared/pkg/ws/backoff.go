package ws

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

// Backoff schedules reconnect attempts.
//
// Every parameter is explicit and every field is required: a reconnect policy
// inherited from a library's defaults is a policy nobody has thought about, and
// the failure mode — every disconnected client retrying in lockstep and
// finishing off the service that just came back — is the one that hurts most.
type Backoff struct {
	// Initial is the delay before the second attempt.
	Initial time.Duration
	// Max caps the delay however long the outage lasts.
	Max time.Duration
	// Factor multiplies the delay after each failure; 2 doubles it.
	Factor float64
	// Jitter is the fraction of a delay that is randomised away, 0 to 1. It is
	// what stops a thousand clients from reconnecting in the same millisecond.
	Jitter float64
}

// DefaultBackoff is a sane starting point: fast enough that a redeploy is
// barely noticed, slow enough that a long outage is not made worse.
var DefaultBackoff = Backoff{
	Initial: 500 * time.Millisecond,
	Max:     30 * time.Second,
	Factor:  2,
	Jitter:  0.2,
}

// ErrInvalidBackoff reports a policy that cannot be used.
var ErrInvalidBackoff = errors.New("invalid backoff")

// Validate checks the policy.
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

// Delay returns how long to wait before the given attempt, counting from zero:
// attempt 0 is the wait after the first failure.
func (b Backoff) Delay(attempt int) time.Duration {
	// G404: this randomness spreads reconnects, it decides nothing about
	// security, and a cryptographic source here would be cargo cult.
	return b.delay(attempt, rand.Float64()) //nolint:gosec
}

// delay is Delay with the randomness passed in, so the arithmetic can be
// tested exactly instead of statistically. random is in [0, 1).
func (b Backoff) delay(attempt int, random float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// math.Pow on large attempt counts overflows into +Inf, which is fine:
	// +Inf is above Max and gets clamped like any other overshoot.
	d := float64(b.Initial) * math.Pow(b.Factor, float64(attempt))
	if d > float64(b.Max) {
		d = float64(b.Max)
	}

	// Jitter only ever subtracts, so Max stays a real ceiling rather than a
	// value the delay hovers around.
	d -= d * b.Jitter * random

	return time.Duration(d)
}

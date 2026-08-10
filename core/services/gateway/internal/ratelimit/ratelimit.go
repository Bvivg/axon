// Package ratelimit throttles requests per caller.
//
// Limiting lives on the gateway because that is the trust boundary: everything
// behind it is internal traffic that has already been through here.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// idleTTL is how long a caller's bucket is kept after their last request.
//
// Eviction is not housekeeping, it is the difference between a limiter and a
// memory leak: without it, one bucket per distinct IP accumulates forever, and
// the component meant to protect the service becomes the way to exhaust it.
const idleTTL = 10 * time.Minute

// sweepInterval is how often idle buckets are collected.
const sweepInterval = time.Minute

// Limiter is a token bucket per caller.
type Limiter struct {
	limit rate.Limit
	burst int

	mu      sync.Mutex
	buckets map[string]*bucket

	now func() time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Config configures a Limiter.
type Config struct {
	// PerMinute is the sustained rate allowed per caller.
	PerMinute int

	// Burst is how many requests may arrive at once before throttling starts.
	// Defaults to a tenth of the per-minute rate, minimum one: a client that
	// batches a few calls on page load should not be punished for it, while a
	// flood still gets shaped.
	Burst int

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// New returns a Limiter. A non-positive rate disables limiting entirely, which
// is what a development environment wants and what production must never have.
func New(cfg Config) *Limiter {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	burst := cfg.Burst
	if burst <= 0 {
		burst = max(cfg.PerMinute/10, 1)
	}

	var limit rate.Limit
	if cfg.PerMinute <= 0 {
		limit = rate.Inf
	} else {
		limit = rate.Limit(float64(cfg.PerMinute) / 60.0)
	}

	return &Limiter{
		limit:   limit,
		burst:   burst,
		buckets: make(map[string]*bucket),
		now:     now,
	}
}

// Allow reports whether the caller may proceed, consuming a token when so.
func (l *Limiter) Allow(key string) bool {
	if l.limit == rate.Inf {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now

	return b.limiter.AllowN(now, 1)
}

// Start sweeps idle buckets until ctx is cancelled.
func (l *Limiter) Start(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.Sweep()
		}
	}
}

// Sweep drops buckets nobody has touched recently. Start calls it on a ticker;
// it is exported so a test can drive eviction without waiting a minute.
func (l *Limiter) Sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-idleTTL)
	for key, b := range l.buckets {
		if b.lastSeen.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Tracked reports how many callers currently have a bucket. Used by tests and
// by the metric that would notice eviction silently stopping.
func (l *Limiter) Tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.buckets)
}

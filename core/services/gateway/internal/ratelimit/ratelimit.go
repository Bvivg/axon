package ratelimit

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const idleTTL = 10 * time.Minute

const sweepInterval = time.Minute

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

type Config struct {
	PerMinute int

	Burst int

	Now func() time.Time
}

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

func (l *Limiter) Tracked() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.buckets)
}

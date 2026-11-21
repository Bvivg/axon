package notify

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

const (
	budgetIdle  = 10 * time.Minute
	budgetSweep = time.Minute
)

type pair [2]uuid.UUID

type pairBudget struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type senderBudget struct {
	limit rate.Limit
	burst int
	now   func() time.Time

	mu    sync.Mutex
	pairs map[pair]*pairBudget
	swept time.Time
}

func newSenderBudget(perMinute, burst int, now func() time.Time) *senderBudget {
	if perMinute <= 0 {
		return nil
	}
	if now == nil {
		now = time.Now
	}
	return &senderBudget{
		limit: rate.Limit(float64(perMinute) / 60),
		burst: max(burst, 1),
		now:   now,
		pairs: map[pair]*pairBudget{},
		swept: now(),
	}
}

func (b *senderBudget) allow(from uuid.UUID, recipients []uuid.UUID) []uuid.UUID {
	if b == nil {
		return recipients
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	now := b.now()
	if now.Sub(b.swept) >= budgetSweep {
		for key, entry := range b.pairs {
			if now.Sub(entry.lastSeen) >= budgetIdle {
				delete(b.pairs, key)
			}
		}
		b.swept = now
	}

	allowed := recipients[:0:0]
	for _, to := range recipients {
		entry, ok := b.pairs[pair{from, to}]
		if !ok {
			entry = &pairBudget{limiter: rate.NewLimiter(b.limit, b.burst)}
			b.pairs[pair{from, to}] = entry
		}
		entry.lastSeen = now
		if entry.limiter.AllowN(now, 1) {
			allowed = append(allowed, to)
		}
	}
	return allowed
}

func (b *senderBudget) tracked() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pairs)
}

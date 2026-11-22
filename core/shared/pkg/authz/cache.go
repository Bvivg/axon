package authz

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Source interface {
	Permissions(ctx context.Context, user uuid.UUID, authorization string) ([]string, error)
}

type SourceFunc func(ctx context.Context, user uuid.UUID, authorization string) ([]string, error)

func (f SourceFunc) Permissions(ctx context.Context, user uuid.UUID, authorization string) ([]string, error) {
	return f(ctx, user, authorization)
}

const (
	DefaultTTL      = 30 * time.Second
	maxCacheEntries = 10000
)

type entry struct {
	set     Set
	expires time.Time
}

type Cache struct {
	source Source
	ttl    time.Duration
	now    func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]entry
}

func NewCache(source Source, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{source: source, ttl: ttl, now: time.Now, entries: make(map[uuid.UUID]entry)}
}

func (c *Cache) Permissions(ctx context.Context, user uuid.UUID, authorization string) (Set, error) {
	now := c.now()
	c.mu.Lock()
	cached, ok := c.entries[user]
	c.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.set, nil
	}

	granted, err := c.source.Permissions(ctx, user, authorization)
	if err != nil {
		return nil, err
	}
	set := NewSet(granted)

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCacheEntries {
		for id, stale := range c.entries {
			if !now.Before(stale.expires) {
				delete(c.entries, id)
			}
		}
		if len(c.entries) >= maxCacheEntries {
			c.entries = make(map[uuid.UUID]entry)
		}
	}
	c.entries[user] = entry{set: set, expires: now.Add(c.ttl)}
	return set, nil
}

func (c *Cache) Forget(user uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, user)
}

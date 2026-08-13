package presence

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const keyPrefix = "presence:session:"

type Tracker struct {
	client goredis.UniversalClient
}

func NewTracker(client goredis.UniversalClient) *Tracker {
	return &Tracker{client: client}
}

func (t *Tracker) Touch(ctx context.Context, sessionID string, ttl time.Duration) error {
	if err := t.client.Set(ctx, key(sessionID), "1", ttl).Err(); err != nil {
		return fmt.Errorf("presence: touch %s: %w", sessionID, err)
	}
	return nil
}

func (t *Tracker) Clear(ctx context.Context, sessionID string) error {
	if err := t.client.Del(ctx, key(sessionID)).Err(); err != nil {
		return fmt.Errorf("presence: clear %s: %w", sessionID, err)
	}
	return nil
}

func (t *Tracker) Online(ctx context.Context, sessionIDs []string) (map[string]bool, error) {
	online := make(map[string]bool, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return online, nil
	}

	pipe := t.client.Pipeline()
	cmds := make(map[string]*goredis.IntCmd, len(sessionIDs))
	for _, id := range sessionIDs {
		cmds[id] = pipe.Exists(ctx, key(id))
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("presence: check online: %w", err)
	}

	for id, cmd := range cmds {
		online[id] = cmd.Val() > 0
	}
	return online, nil
}

func key(sessionID string) string {
	return keyPrefix + sessionID
}

package presence

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const keyPrefix = "presence:conns:"

type Tracker struct {
	client goredis.UniversalClient
	now    func() time.Time
}

func NewTracker(client goredis.UniversalClient) *Tracker {
	return &Tracker{client: client, now: time.Now}
}

func (t *Tracker) Touch(ctx context.Context, sessionID, connID string, ttl time.Duration) error {
	now := t.now()
	k := key(sessionID)

	pipe := t.client.TxPipeline()
	pipe.ZAdd(ctx, k, goredis.Z{Score: float64(now.Add(ttl).UnixMilli()), Member: connID})
	pipe.ZRemRangeByScore(ctx, k, "-inf", "("+strconv.FormatInt(now.UnixMilli(), 10))
	pipe.PExpire(ctx, k, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("presence: touch %s: %w", sessionID, err)
	}
	return nil
}

func (t *Tracker) Clear(ctx context.Context, sessionID, connID string) error {
	if err := t.client.ZRem(ctx, key(sessionID), connID).Err(); err != nil {
		return fmt.Errorf("presence: clear %s: %w", sessionID, err)
	}
	return nil
}

func (t *Tracker) Online(ctx context.Context, sessionIDs []string) (map[string]bool, error) {
	online := make(map[string]bool, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return online, nil
	}

	from := strconv.FormatInt(t.now().UnixMilli(), 10)

	pipe := t.client.Pipeline()
	cmds := make(map[string]*goredis.IntCmd, len(sessionIDs))
	for _, id := range sessionIDs {
		cmds[id] = pipe.ZCount(ctx, key(id), from, "+inf")
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

const changeChannelPrefix = "presence:changed:"

func changeChannel(userID string) string {
	return changeChannelPrefix + userID
}

func (t *Tracker) PublishChanged(ctx context.Context, userID string) error {
	if err := t.client.Publish(ctx, changeChannel(userID), "changed").Err(); err != nil {
		return fmt.Errorf("presence: publish change for %s: %w", userID, err)
	}
	return nil
}

func (t *Tracker) SubscribeChanged(ctx context.Context, userID string) (<-chan struct{}, func(), error) {
	sub := t.client.Subscribe(ctx, changeChannel(userID))
	if _, err := sub.Receive(ctx); err != nil {
		_ = sub.Close()
		return nil, nil, fmt.Errorf("presence: subscribe changes for %s: %w", userID, err)
	}

	changed := make(chan struct{}, 1)

	go func() {
		defer close(changed)
		for range sub.Channel() {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	}()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = sub.Close()
		})
	}

	return changed, stop, nil
}

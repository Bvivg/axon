package throttle

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type LoginConfig struct {
	Threshold int
	BaseDelay time.Duration
	MaxDelay  time.Duration
	Window    time.Duration
}

func DefaultLoginConfig() LoginConfig {
	return LoginConfig{
		Threshold: 5,
		BaseDelay: 15 * time.Second,
		MaxDelay:  15 * time.Minute,
		Window:    time.Hour,
	}
}

func (c LoginConfig) orDefaults() LoginConfig {
	d := DefaultLoginConfig()
	if c.Threshold <= 0 {
		c.Threshold = d.Threshold
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = d.BaseDelay
	}
	if c.MaxDelay < c.BaseDelay {
		c.MaxDelay = max(d.MaxDelay, c.BaseDelay)
	}
	if c.Window <= 0 {
		c.Window = d.Window
	}
	return c
}

type Logins struct {
	client redis.UniversalClient
	cfg    LoginConfig
}

func NewLogins(client redis.UniversalClient, cfg LoginConfig) *Logins {
	return &Logins{client: client, cfg: cfg.orDefaults()}
}

var recordFailure = redis.NewScript(`
local failures = redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ARGV[1])
local threshold = tonumber(ARGV[2])
if failures < threshold then
  return 0
end
local delay = tonumber(ARGV[3])
local ceiling = tonumber(ARGV[4])
local steps = failures - threshold
while steps > 0 and delay < ceiling do
  delay = delay * 2
  steps = steps - 1
end
if delay > ceiling then
  delay = ceiling
end
redis.call('SET', KEYS[2], '1', 'PX', delay)
return delay
`)

func failuresKey(account string) string { return "auth:login:{" + account + "}:failures" }

func lockKey(account string) string { return "auth:login:{" + account + "}:lock" }

func (l *Logins) Blocked(ctx context.Context, account string) (time.Duration, error) {
	remaining, err := l.client.PTTL(ctx, lockKey(account)).Result()
	if err != nil {
		return 0, fmt.Errorf("throttle: read the sign-in lock: %w", err)
	}
	return max(remaining, 0), nil
}

func (l *Logins) Failed(ctx context.Context, account string) (time.Duration, error) {
	delay, err := recordFailure.Run(ctx, l.client,
		[]string{failuresKey(account), lockKey(account)},
		l.cfg.Window.Milliseconds(), l.cfg.Threshold, l.cfg.BaseDelay.Milliseconds(), l.cfg.MaxDelay.Milliseconds(),
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("throttle: record a failed sign-in: %w", err)
	}
	return time.Duration(delay) * time.Millisecond, nil
}

func (l *Logins) Succeeded(ctx context.Context, account string) error {
	if err := l.client.Del(ctx, failuresKey(account), lockKey(account)).Err(); err != nil {
		return fmt.Errorf("throttle: clear the sign-in failures: %w", err)
	}
	return nil
}

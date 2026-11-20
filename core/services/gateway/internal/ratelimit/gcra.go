package ratelimit

import (
	"context"
	"math"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var gcraScript = goredis.NewScript(`
local now_parts = redis.call('TIME')
local now = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local emission = tonumber(ARGV[1])
local tolerance = tonumber(ARGV[2])
local tat = tonumber(redis.call('GET', KEYS[1]) or now)
if tat < now then
  tat = now
end
local next_tat = tat + emission
local allow_at = next_tat - tolerance
if allow_at > now then
  return {0, math.ceil(allow_at - now)}
end
redis.call('SET', KEYS[1], next_tat, 'PX', math.ceil(next_tat - now))
return {1, 0}
`)

type Store interface {
	Take(ctx context.Context, key string, emission, tolerance time.Duration) (bool, time.Duration, error)
}

type Redis struct {
	client goredis.UniversalClient
}

func NewRedis(client goredis.UniversalClient) *Redis { return &Redis{client: client} }

func (r *Redis) Take(ctx context.Context, key string, emission, tolerance time.Duration) (bool, time.Duration, error) {
	result, err := gcraScript.Run(ctx, r.client, []string{key}, emission.Milliseconds(), tolerance.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	return result[0] == 1, time.Duration(result[1]) * time.Millisecond, nil
}

func (l *Limiter) remote(key string) (bool, time.Duration, bool) {
	if l.store == nil {
		return false, 0, false
	}
	emission := time.Duration(math.Round(float64(time.Second) / float64(l.limit)))
	tolerance := emission * time.Duration(l.burst)
	ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
	defer cancel()
	allowed, retry, err := l.store.Take(ctx, "rl:"+l.name+":"+key, emission, tolerance)
	if err != nil {
		if l.failures != nil {
			l.failures.Inc()
		}
		return false, 0, false
	}
	return allowed, retry, true
}

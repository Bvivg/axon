package ratelimit_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

func TestInstancesShareOneBudgetThroughRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := ratelimit.NewRedis(client)
	first := ratelimit.New(ratelimit.Config{PerMinute: 60, Burst: 5, Name: "login", Store: store})
	second := ratelimit.New(ratelimit.Config{PerMinute: 60, Burst: 5, Name: "login", Store: store})
	allowed := 0
	for i := range 20 {
		limiter := first
		if i%2 == 1 {
			limiter = second
		}
		if limiter.Allow("203.0.113.7") {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("two gateways allowed %d requests, want the shared burst of 5", allowed)
	}
	ok, retry := first.AllowWithRetry("203.0.113.7")
	if ok || retry <= 0 || retry > time.Second {
		t.Fatal("retry hint", ok, retry)
	}
	if !first.Allow("198.51.100.1") {
		t.Fatal("another client must have its own budget")
	}
}

func TestFallsBackToLocalLimitWhenRedisIsDown(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	limiter := ratelimit.New(ratelimit.Config{PerMinute: 60, Burst: 2, Name: "login", Store: ratelimit.NewRedis(client), Timeout: 20 * time.Millisecond})
	server.Close()
	allowed := 0
	for range 5 {
		if limiter.Allow("203.0.113.7") {
			allowed++
		}
	}
	if allowed != 2 {
		t.Fatalf("local fallback allowed %d, want 2", allowed)
	}
}

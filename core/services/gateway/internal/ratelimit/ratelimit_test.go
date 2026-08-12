package ratelimit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

func TestAllowsUpToTheBurstThenThrottles(t *testing.T) {
	clock := time.Now()
	l := ratelimit.New(ratelimit.Config{
		PerMinute: 60, Burst: 5, Now: func() time.Time { return clock },
	})

	for i := range 5 {
		if !l.Allow("caller") {
			t.Fatalf("request %d was refused inside the burst", i+1)
		}
	}
	if l.Allow("caller") {
		t.Fatal("the request past the burst was allowed")
	}
}

func TestTokensRefillOverTime(t *testing.T) {
	clock := time.Now()
	l := ratelimit.New(ratelimit.Config{
		PerMinute: 60, Burst: 1, Now: func() time.Time { return clock },
	})

	if !l.Allow("caller") {
		t.Fatal("the first request was refused")
	}
	if l.Allow("caller") {
		t.Fatal("a second immediate request was allowed with a burst of one")
	}

	clock = clock.Add(time.Second)

	if !l.Allow("caller") {
		t.Fatal("no token had refilled after a second")
	}
}

func TestCallersAreIndependent(t *testing.T) {
	clock := time.Now()
	l := ratelimit.New(ratelimit.Config{
		PerMinute: 60, Burst: 2, Now: func() time.Time { return clock },
	})

	for range 2 {
		l.Allow("noisy")
	}
	if l.Allow("noisy") {
		t.Fatal("the noisy caller was not throttled")
	}

	if !l.Allow("quiet") {
		t.Fatal("a different caller was throttled by someone else's traffic")
	}
}

func TestIdleBucketsAreEvicted(t *testing.T) {
	clock := time.Now()
	l := ratelimit.New(ratelimit.Config{
		PerMinute: 60, Burst: 1, Now: func() time.Time { return clock },
	})

	for i := range 100 {
		l.Allow(string(rune('a'+i%26)) + string(rune('a'+i/26)))
	}
	if got := l.Tracked(); got != 100 {
		t.Fatalf("tracking %d callers, want 100", got)
	}

	clock = clock.Add(time.Hour)
	l.Allow("aa")

	l.Sweep()

	if got := l.Tracked(); got != 1 {
		t.Fatalf("tracking %d callers after the idle window, want only the active one", got)
	}
}

func TestNonPositiveRateDisablesLimiting(t *testing.T) {
	l := ratelimit.New(ratelimit.Config{PerMinute: 0})

	for i := range 1000 {
		if !l.Allow("caller") {
			t.Fatalf("request %d was refused with limiting disabled", i)
		}
	}
	if got := l.Tracked(); got != 0 {
		t.Errorf("a disabled limiter allocated %d buckets", got)
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name           string
		remoteAddr     string
		forwardedFor   string
		trustedProxies int
		want           string
	}{
		{
			name:       "no proxy, header ignored",
			remoteAddr: "203.0.113.7:54321",

			forwardedFor:   "1.2.3.4",
			trustedProxies: 0,
			want:           "203.0.113.7",
		},
		{
			name:           "one trusted proxy",
			remoteAddr:     "10.0.0.1:443",
			forwardedFor:   "203.0.113.7",
			trustedProxies: 1,
			want:           "203.0.113.7",
		},
		{

			name:           "spoofed entries are ignored",
			remoteAddr:     "10.0.0.1:443",
			forwardedFor:   "1.2.3.4, 5.6.7.8, 203.0.113.7",
			trustedProxies: 1,
			want:           "203.0.113.7",
		},
		{
			name:           "two trusted proxies",
			remoteAddr:     "10.0.0.1:443",
			forwardedFor:   "203.0.113.7, 10.0.0.2",
			trustedProxies: 2,
			want:           "203.0.113.7",
		},
		{
			name:           "header shorter than the expected chain",
			remoteAddr:     "10.0.0.1:443",
			forwardedFor:   "203.0.113.7",
			trustedProxies: 3,
			want:           "10.0.0.1",
		},
		{
			name:           "garbage in the header",
			remoteAddr:     "10.0.0.1:443",
			forwardedFor:   "not-an-ip",
			trustedProxies: 1,
			want:           "10.0.0.1",
		},
		{
			name:           "no header at all",
			remoteAddr:     "10.0.0.1:443",
			trustedProxies: 1,
			want:           "10.0.0.1",
		},
		{
			name:       "ipv6",
			remoteAddr: "[2001:db8::1]:443",
			want:       "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httpRequest(tt.remoteAddr, tt.forwardedFor)

			if got := ratelimit.ClientIP(req, tt.trustedProxies); got != tt.want {
				t.Fatalf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func httpRequest(remoteAddr, forwardedFor string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "http://gateway/x", nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set(ratelimit.ForwardedForHeader, forwardedFor)
	}
	return req
}

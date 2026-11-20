package throttle_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/auth/internal/throttle"
)

func newLogins(t *testing.T, cfg throttle.LoginConfig) (*throttle.Logins, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	return throttle.NewLogins(client, cfg), server
}

var testConfig = throttle.LoginConfig{
	Threshold: 3,
	BaseDelay: 10 * time.Second,
	MaxDelay:  45 * time.Second,
	Window:    time.Hour,
}

func TestFailuresBelowTheThresholdDoNotBlock(t *testing.T) {
	logins, _ := newLogins(t, testConfig)

	for i := range testConfig.Threshold - 1 {
		delay, err := logins.Failed(t.Context(), "account")
		if err != nil {
			t.Fatalf("Failed #%d: %v", i+1, err)
		}
		if delay != 0 {
			t.Fatalf("failure #%d locked the account for %v", i+1, delay)
		}
	}

	if blocked, err := logins.Blocked(t.Context(), "account"); err != nil || blocked != 0 {
		t.Fatalf("Blocked = %v, %v; want open", blocked, err)
	}
}

func TestTheLockDoublesUpToItsCap(t *testing.T) {
	logins, _ := newLogins(t, testConfig)

	for range testConfig.Threshold - 1 {
		if _, err := logins.Failed(t.Context(), "account"); err != nil {
			t.Fatal(err)
		}
	}

	for i, want := range []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 45 * time.Second, 45 * time.Second} {
		delay, err := logins.Failed(t.Context(), "account")
		if err != nil {
			t.Fatal(err)
		}
		if delay != want {
			t.Fatalf("lock after failure %d = %v, want %v", testConfig.Threshold+i, delay, want)
		}
		blocked, err := logins.Blocked(t.Context(), "account")
		if err != nil {
			t.Fatal(err)
		}
		if blocked <= 0 || blocked > want {
			t.Fatalf("Blocked = %v, want within (0, %v]", blocked, want)
		}
	}
}

func TestALockExpires(t *testing.T) {
	logins, server := newLogins(t, testConfig)

	for range testConfig.Threshold {
		if _, err := logins.Failed(t.Context(), "account"); err != nil {
			t.Fatal(err)
		}
	}

	server.FastForward(testConfig.BaseDelay + time.Second)

	if blocked, err := logins.Blocked(t.Context(), "account"); err != nil || blocked != 0 {
		t.Fatalf("Blocked = %v, %v after the lock elapsed; want open", blocked, err)
	}
}

func TestTheFailureCountForgetsAfterTheWindow(t *testing.T) {
	logins, server := newLogins(t, testConfig)

	for range testConfig.Threshold - 1 {
		if _, err := logins.Failed(t.Context(), "account"); err != nil {
			t.Fatal(err)
		}
	}

	server.FastForward(testConfig.Window + time.Second)

	if delay, err := logins.Failed(t.Context(), "account"); err != nil || delay != 0 {
		t.Fatalf("a failure after the window locked the account for %v (%v)", delay, err)
	}
}

func TestASuccessResetsTheAccount(t *testing.T) {
	logins, _ := newLogins(t, testConfig)

	for range testConfig.Threshold {
		if _, err := logins.Failed(t.Context(), "account"); err != nil {
			t.Fatal(err)
		}
	}
	if err := logins.Succeeded(t.Context(), "account"); err != nil {
		t.Fatalf("Succeeded: %v", err)
	}

	if blocked, err := logins.Blocked(t.Context(), "account"); err != nil || blocked != 0 {
		t.Fatalf("Blocked = %v, %v after a success; want open", blocked, err)
	}
	if delay, err := logins.Failed(t.Context(), "account"); err != nil || delay != 0 {
		t.Fatalf("the count survived a success: lock %v (%v)", delay, err)
	}
}

func TestAccountsAreThrottledSeparately(t *testing.T) {
	logins, _ := newLogins(t, testConfig)

	for range testConfig.Threshold {
		if _, err := logins.Failed(t.Context(), "noisy"); err != nil {
			t.Fatal(err)
		}
	}

	if blocked, err := logins.Blocked(t.Context(), "quiet"); err != nil || blocked != 0 {
		t.Fatalf("another account is blocked for %v (%v)", blocked, err)
	}
}

func TestMissingSettingsFallBackToDefaults(t *testing.T) {
	logins, _ := newLogins(t, throttle.LoginConfig{})
	defaults := throttle.DefaultLoginConfig()

	var delay time.Duration
	for range defaults.Threshold {
		var err error
		if delay, err = logins.Failed(t.Context(), "account"); err != nil {
			t.Fatal(err)
		}
	}
	if delay != defaults.BaseDelay {
		t.Fatalf("lock = %v, want the default %v", delay, defaults.BaseDelay)
	}
}

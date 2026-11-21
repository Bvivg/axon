package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/limit"
	"github.com/bvivg/axon/core/services/auth/internal/service"
	"github.com/bvivg/axon/core/services/auth/internal/throttle"
)

var throttleConfig = throttle.LoginConfig{
	Threshold: 3,
	BaseDelay: 30 * time.Second,
	MaxDelay:  10 * time.Minute,
	Window:    time.Hour,
}

func withLoginThrottle(t *testing.T) (harnessOption, *miniredis.Miniredis) {
	t.Helper()

	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 200 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })

	logins := throttle.NewLogins(client, throttleConfig)
	return func(cfg *service.Config) { cfg.LoginThrottle = logins }, server
}

func withPasswordGate(gate *limit.Gate) harnessOption {
	return func(cfg *service.Config) { cfg.PasswordGate = gate }
}

func failLogins(t *testing.T, h *harness, email string, n int) {
	t.Helper()

	for i := range n {
		_, err := h.svc.Login(context.Background(), service.LoginInput{Email: email, Password: "not the password at all"})
		if !errors.Is(err, domain.ErrInvalidCredentials) {
			t.Fatalf("failure #%d: err = %v, want ErrInvalidCredentials", i+1, err)
		}
	}
}

func TestRepeatedFailuresLockTheAccountEvenAgainstTheRightPassword(t *testing.T) {
	opt, redis := withLoginThrottle(t)
	h := newHarness(t, opt)
	h.register(t, "ada@example.com", validPassword)

	failLogins(t, h, "ada@example.com", throttleConfig.Threshold)

	_, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ADA@example.com", Password: validPassword})
	var throttled *domain.ThrottledError
	if !errors.As(err, &throttled) || !errors.Is(err, domain.ErrLoginThrottled) {
		t.Fatalf("err = %v, want a ThrottledError", err)
	}
	if throttled.RetryAfter <= 0 || throttled.RetryAfter > throttleConfig.BaseDelay {
		t.Errorf("RetryAfter = %v, want within (0, %v]", throttled.RetryAfter, throttleConfig.BaseDelay)
	}

	redis.FastForward(throttleConfig.BaseDelay + time.Second)

	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword}); err != nil {
		t.Fatalf("Login after the lock: %v", err)
	}
	failLogins(t, h, "ada@example.com", throttleConfig.Threshold-1)
	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword}); err != nil {
		t.Fatalf("a success did not reset the count: %v", err)
	}
}

func TestAThrottledSignInSpendsNoHashing(t *testing.T) {
	opt, _ := withLoginThrottle(t)
	gate := limit.NewGate(1, 0)
	h := newHarness(t, opt, withPasswordGate(gate))
	h.register(t, "ada@example.com", validPassword)

	failLogins(t, h, "ada@example.com", throttleConfig.Threshold)

	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold the hashing slot: %v", err)
	}
	defer release()

	_, err = h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword})
	if !errors.Is(err, domain.ErrLoginThrottled) {
		t.Fatalf("err = %v, want ErrLoginThrottled before any hashing", err)
	}
}

func TestUnknownAccountsAreThrottledLikeRealOnes(t *testing.T) {
	opt, _ := withLoginThrottle(t)
	h := newHarness(t, opt)

	failLogins(t, h, "nobody@example.com", throttleConfig.Threshold)

	_, err := h.svc.Login(context.Background(), service.LoginInput{Email: "nobody@example.com", Password: validPassword})
	if !errors.Is(err, domain.ErrLoginThrottled) {
		t.Fatalf("err = %v, want ErrLoginThrottled", err)
	}
}

func TestOneAccountsFailuresDoNotLockAnother(t *testing.T) {
	opt, _ := withLoginThrottle(t)
	h := newHarness(t, opt)
	h.register(t, "grace@example.com", validPassword)

	failLogins(t, h, "ada@example.com", throttleConfig.Threshold)

	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "grace@example.com", Password: validPassword}); err != nil {
		t.Fatalf("an unrelated account was throttled: %v", err)
	}
}

func TestSignInKeepsWorkingWhenTheThrottleStoreIsDown(t *testing.T) {
	opt, redis := withLoginThrottle(t)
	h := newHarness(t, opt)
	h.register(t, "ada@example.com", validPassword)

	redis.Close()

	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword}); err != nil {
		t.Fatalf("Login with the throttle store down: %v", err)
	}
}

func TestPasswordWorkFailsFastWhenHashingIsSaturated(t *testing.T) {
	gate := limit.NewGate(1, 0)
	h := newHarness(t, withPasswordGate(gate))
	h.register(t, "ada@example.com", validPassword)

	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold the hashing slot: %v", err)
	}

	start := time.Now()
	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword}); !errors.Is(err, domain.ErrPasswordHashingBusy) {
		t.Errorf("Login err = %v, want ErrPasswordHashingBusy", err)
	}
	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "nobody@example.com", Password: validPassword}); !errors.Is(err, domain.ErrPasswordHashingBusy) {
		t.Errorf("Login of an unknown account err = %v, want ErrPasswordHashingBusy", err)
	}
	if _, err := h.svc.Register(context.Background(), service.RegisterInput{Email: "eve@example.com", Password: validPassword}); !errors.Is(err, domain.ErrPasswordHashingBusy) {
		t.Errorf("Register err = %v, want ErrPasswordHashingBusy", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("saturated hashing held the requests for %v", waited)
	}

	release()
	if _, err := h.svc.Login(context.Background(), service.LoginInput{Email: "ada@example.com", Password: validPassword}); err != nil {
		t.Fatalf("Login after the slot freed: %v", err)
	}
}

package resilience_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/bvivg/axon/core/shared/pkg/resilience"
)

const procedure = "/axon.test.v1.Probe/Get"

type server struct {
	calls atomic.Int32
	fail  func(call int32) error
	delay time.Duration
}

func (s *server) start(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(procedure, connect.NewUnaryHandler(procedure, func(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
		call := s.calls.Add(1)
		if s.delay > 0 {
			select {
			case <-time.After(s.delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if s.fail != nil {
			if err := s.fail(call); err != nil {
				return nil, err
			}
		}
		return connect.NewResponse(&emptypb.Empty{}), nil
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func client(url string, interceptor *resilience.Interceptor, idempotency connect.IdempotencyLevel) *connect.Client[emptypb.Empty, emptypb.Empty] {
	return connect.NewClient[emptypb.Empty, emptypb.Empty](http.DefaultClient, url+procedure,
		connect.WithInterceptors(interceptor), connect.WithIdempotency(idempotency))
}

func noSleep(context.Context, time.Duration) error { return nil }

func call(c *connect.Client[emptypb.Empty, emptypb.Empty]) error {
	_, err := c.CallUnary(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	return err
}

func unavailable(int32) error { return connect.NewError(connect.CodeUnavailable, errors.New("down")) }

func TestRetriesTransientFailuresOfIdempotentCalls(t *testing.T) {
	s := &server{fail: func(call int32) error {
		if call == 1 {
			return unavailable(call)
		}
		return nil
	}}
	url := s.start(t)
	i := resilience.New(resilience.Options{Sleep: noSleep})
	if err := call(client(url, i, connect.IdempotencyNoSideEffects)); err != nil {
		t.Fatal(err)
	}
	if s.calls.Load() != 2 {
		t.Fatal("calls", s.calls.Load())
	}
}

func TestDoesNotRetryCallsWithSideEffects(t *testing.T) {
	s := &server{fail: unavailable}
	url := s.start(t)
	i := resilience.New(resilience.Options{Sleep: noSleep})
	if err := call(client(url, i, connect.IdempotencyUnknown)); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatal(err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("calls", s.calls.Load())
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	s := &server{fail: func(int32) error { return connect.NewError(connect.CodeInvalidArgument, errors.New("bad")) }}
	url := s.start(t)
	i := resilience.New(resilience.Options{Sleep: noSleep})
	if err := call(client(url, i, connect.IdempotencyIdempotent)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal(err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("calls", s.calls.Load())
	}
}

func TestBreakerOpensThenProbesAndCloses(t *testing.T) {
	healthy := atomic.Bool{}
	s := &server{fail: func(call int32) error {
		if healthy.Load() {
			return nil
		}
		return unavailable(call)
	}}
	url := s.start(t)
	now := &clock{now: time.Unix(1_000, 0)}
	var states []resilience.State
	i := resilience.New(resilience.Options{Sleep: noSleep, Attempts: 1, Failures: 3, Cooldown: 5 * time.Second, Now: now.Now,
		OnState: func(_ string, state resilience.State) { states = append(states, state) }})
	c := client(url, i, connect.IdempotencyUnknown)
	for range 3 {
		_ = call(c)
	}
	if i.State(procedure) != resilience.Open {
		t.Fatal("breaker did not open", i.State(procedure))
	}
	before := s.calls.Load()
	if err := call(c); !errors.Is(err, resilience.ErrOpen) || s.calls.Load() != before {
		t.Fatal("open breaker let the call through", err)
	}
	now.advance(6 * time.Second)
	healthy.Store(true)
	if err := call(c); err != nil {
		t.Fatal("half-open probe failed", err)
	}
	if i.State(procedure) != resilience.Closed {
		t.Fatal("breaker did not close", i.State(procedure))
	}
	want := []resilience.State{resilience.Open, resilience.HalfOpen, resilience.Closed}
	if len(states) != len(want) {
		t.Fatal(states)
	}
	for k := range want {
		if states[k] != want[k] {
			t.Fatal(states)
		}
	}
}

func TestRetryBudgetCapsRetryStorms(t *testing.T) {
	s := &server{fail: unavailable}
	url := s.start(t)
	now := &clock{now: time.Unix(1_000, 0)}
	i := resilience.New(resilience.Options{Sleep: noSleep, Attempts: 3, Failures: 1_000, BudgetMinimum: 2, BudgetRatio: 0.1, Now: now.Now})
	c := client(url, i, connect.IdempotencyNoSideEffects)
	for range 10 {
		_ = call(c)
	}
	if got := s.calls.Load(); got >= 20 {
		t.Fatal("retries were not capped by the budget", got)
	}
}

func TestDefaultTimeoutApplies(t *testing.T) {
	s := &server{delay: time.Second}
	url := s.start(t)
	i := resilience.New(resilience.Options{Sleep: noSleep, Attempts: 1, Timeout: 50 * time.Millisecond})
	started := time.Now()
	if err := call(client(url, i, connect.IdempotencyUnknown)); connect.CodeOf(err) != connect.CodeDeadlineExceeded {
		t.Fatal(err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("timeout was not applied")
	}
}

package resilience

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"connectrpc.com/connect"
)

var ErrOpen = errors.New("resilience: circuit is open")

type Options struct {
	Timeout       time.Duration
	Attempts      int
	BaseBackoff   time.Duration
	MaxBackoff    time.Duration
	BudgetRatio   float64
	BudgetMinimum float64
	Failures      int
	Cooldown      time.Duration
	Now           func() time.Time
	Sleep         func(context.Context, time.Duration) error
	OnState       func(procedure string, state State)
}

type State string

const (
	Closed   State = "closed"
	Open     State = "open"
	HalfOpen State = "half_open"
)

func (o *Options) defaults() {
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.Attempts <= 0 {
		o.Attempts = 3
	}
	if o.BaseBackoff <= 0 {
		o.BaseBackoff = 50 * time.Millisecond
	}
	if o.MaxBackoff <= 0 {
		o.MaxBackoff = time.Second
	}
	if o.BudgetRatio <= 0 {
		o.BudgetRatio = 0.1
	}
	if o.BudgetMinimum <= 0 {
		o.BudgetMinimum = 3
	}
	if o.Failures <= 0 {
		o.Failures = 5
	}
	if o.Cooldown <= 0 {
		o.Cooldown = 5 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Interceptor struct {
	opts     Options
	budget   *budget
	mu       sync.Mutex
	breakers map[string]*breaker
}

func New(opts Options) *Interceptor {
	opts.defaults()
	return &Interceptor{opts: opts, budget: newBudget(opts.BudgetRatio, opts.BudgetMinimum, opts.Now), breakers: map[string]*breaker{}}
}

func (i *Interceptor) breaker(procedure string) *breaker {
	i.mu.Lock()
	defer i.mu.Unlock()
	b := i.breakers[procedure]
	if b == nil {
		b = &breaker{procedure: procedure, opts: &i.opts, state: Closed}
		i.breakers[procedure] = b
	}
	return b
}

func (i *Interceptor) State(procedure string) State {
	b := i.breaker(procedure)
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.current()
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if !req.Spec().IsClient {
			return next(ctx, req)
		}
		procedure := req.Spec().Procedure
		b := i.breaker(procedure)
		retryable := req.Spec().IdempotencyLevel != connect.IdempotencyUnknown
		i.budget.request()
		var lastErr error
		for attempt := 0; attempt < i.opts.Attempts; attempt++ {
			if attempt > 0 {
				if !retryable || !transient(lastErr) || !i.budget.withdraw() {
					return nil, lastErr
				}
				if err := i.opts.Sleep(ctx, i.backoff(attempt)); err != nil {
					return nil, lastErr
				}
			}
			if !b.allow() {
				return nil, connect.NewError(connect.CodeUnavailable, ErrOpen)
			}
			call, cancel := ctx, context.CancelFunc(func() {})
			if _, ok := ctx.Deadline(); !ok {
				call, cancel = context.WithTimeout(ctx, i.opts.Timeout)
			}
			resp, err := next(call, req)
			cancel()
			b.record(err)
			if err == nil {
				return resp, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, err
			}
		}
		return nil, lastErr
	}
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (i *Interceptor) backoff(attempt int) time.Duration {
	ceiling := min(i.opts.MaxBackoff, i.opts.BaseBackoff<<min(attempt-1, 16))
	return time.Duration(rand.Int64N(int64(ceiling)) + 1) //nolint:gosec
}

func transient(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeResourceExhausted, connect.CodeAborted:
		return !errors.Is(err, ErrOpen)
	default:
		return false
	}
}

func failure(err error) bool {
	if err == nil {
		return false
	}
	switch connect.CodeOf(err) {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeInternal, connect.CodeUnknown, connect.CodeResourceExhausted:
		return true
	default:
		return false
	}
}

type breaker struct {
	procedure string
	opts      *Options
	mu        sync.Mutex
	state     State
	failures  int
	openedAt  time.Time
	probing   bool
}

func (b *breaker) current() State {
	if b.state == Open && b.opts.Now().Sub(b.openedAt) >= b.opts.Cooldown {
		return HalfOpen
	}
	return b.state
}

func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.current() {
	case Closed:
		return true
	case HalfOpen:
		if b.probing {
			return false
		}
		b.transition(HalfOpen)
		b.probing = true
		return true
	default:
		return false
	}
}

func (b *breaker) record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	failed := failure(err)
	if b.state == HalfOpen {
		b.probing = false
		if failed {
			b.trip()
		} else {
			b.failures = 0
			b.transition(Closed)
		}
		return
	}
	if !failed {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures >= b.opts.Failures {
		b.trip()
	}
}

func (b *breaker) trip() {
	b.openedAt = b.opts.Now()
	b.failures = 0
	b.transition(Open)
}

func (b *breaker) transition(state State) {
	if b.state == state {
		return
	}
	b.state = state
	if b.opts.OnState != nil {
		b.opts.OnState(b.procedure, state)
	}
}

type budget struct {
	mu      sync.Mutex
	ratio   float64
	minimum float64
	tokens  float64
	last    time.Time
	now     func() time.Time
}

func newBudget(ratio, minimum float64, now func() time.Time) *budget {
	return &budget{ratio: ratio, minimum: minimum, tokens: minimum, last: now(), now: now}
}

func (b *budget) refill() {
	now := b.now()
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens = min(b.tokens+elapsed*b.minimum, b.minimum*10)
}

func (b *budget) request() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	b.tokens = min(b.tokens+b.ratio, b.minimum*10)
}

func (b *budget) withdraw() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

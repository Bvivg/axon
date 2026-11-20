package uci

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func starter(searches [][]string, scripts ...script) (Starter, *atomic.Int32) {
	var started atomic.Int32
	return func(ctx context.Context) (*Engine, error) {
		n := int(started.Add(1)) - 1
		s := scripts[min(n, len(scripts)-1)]
		s.searches = searches
		engine, _, err := startFake(ctx, s)
		return engine, err
	}, &started
}

func TestPoolRestartsAnEngineThatExitedAndRetriesThePosition(t *testing.T) {
	start, started := starter(recorded(t), script{die: true}, script{})
	var restarts atomic.Int32
	pool, err := NewPool(t.Context(), PoolConfig{Size: 1, Depth: 12, Timeout: time.Second, Start: start, Backoff: time.Millisecond,
		OnRestart: func() { restarts.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	got, err := pool.Evaluate(t.Context(), scholar[:5])
	if err != nil {
		t.Fatal(err)
	}
	if got.BestMove != "g7g6" || restarts.Load() != 1 || started.Load() != 2 || pool.Alive() != 1 || pool.Name() != "Fakefish 1" {
		t.Fatalf("result %+v, restarts %d, started %d, alive %d", got, restarts.Load(), started.Load(), pool.Alive())
	}
	if err := pool.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPoolReportsATimedOutPosition(t *testing.T) {
	start, _ := starter(recorded(t), script{hang: true})
	pool, err := NewPool(t.Context(), PoolConfig{Size: 1, Depth: 12, Timeout: 30 * time.Millisecond, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Evaluate(t.Context(), scholar[:3]); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if pool.Alive() != 1 {
		t.Fatalf("an engine that answered stop was replaced: alive %d", pool.Alive())
	}
}

func TestPoolLimitsConcurrentSearches(t *testing.T) {
	var running, peak atomic.Int32
	start, _ := starter(recorded(t), script{delay: 20 * time.Millisecond, running: &running, peak: &peak})
	pool, err := NewPool(t.Context(), PoolConfig{Size: 2, Depth: 12, Timeout: time.Second, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var wg sync.WaitGroup
	for ply := range 8 {
		wg.Go(func() {
			if _, err := pool.Evaluate(t.Context(), scholar[:ply%len(scholar)]); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if peak.Load() != 2 {
		t.Fatalf("peak concurrent searches = %d, want 2", peak.Load())
	}
}

func TestPoolWaitsForAnEngineOnlyUntilTheCaller(t *testing.T) {
	start, _ := starter(recorded(t), script{hang: true})
	pool, err := NewPool(t.Context(), PoolConfig{Size: 1, Depth: 12, Timeout: time.Second, Start: start})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	busy := make(chan error, 1)
	go func() {
		_, err := pool.Evaluate(t.Context(), nil)
		busy <- err
	}()
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := pool.Evaluate(ctx, nil); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTimeout) {
		t.Fatalf("waiting caller err = %v", err)
	}
	if err := <-busy; !errors.Is(err, ErrTimeout) {
		t.Fatalf("busy caller err = %v", err)
	}
}

func TestNewPoolValidates(t *testing.T) {
	start, _ := starter(recorded(t), script{})
	for name, cfg := range map[string]PoolConfig{
		"size":    {Size: 0, Depth: 12, Timeout: time.Second, Start: start},
		"depth":   {Size: 1, Depth: 0, Timeout: time.Second, Start: start},
		"timeout": {Size: 1, Depth: 12, Start: start},
		"starter": {Size: 1, Depth: 12, Timeout: time.Second},
	} {
		if _, err := NewPool(t.Context(), cfg); err == nil {
			t.Errorf("%s: invalid config accepted", name)
		}
	}
	failing := func(context.Context) (*Engine, error) { return nil, errors.New("no binary") }
	if _, err := NewPool(t.Context(), PoolConfig{Size: 2, Depth: 12, Timeout: time.Second, Start: failing}); err == nil {
		t.Fatal("a pool without engines started")
	}
}

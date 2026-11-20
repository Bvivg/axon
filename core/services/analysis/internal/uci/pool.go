package uci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var ErrNoEngines = errors.New("uci: no engine is running")

type Starter func(ctx context.Context) (*Engine, error)

type PoolConfig struct {
	Size       int
	Depth      int
	Timeout    time.Duration
	Start      Starter
	OnRestart  func()
	Backoff    time.Duration
	MaxBackoff time.Duration
	Log        *slog.Logger
}

type Pool struct {
	cfg     PoolConfig
	ctx     context.Context
	idle    chan *Engine
	alive   atomic.Int64
	name    string
	pending sync.WaitGroup
}

func NewPool(ctx context.Context, cfg PoolConfig) (*Pool, error) {
	switch {
	case cfg.Size < 1:
		return nil, fmt.Errorf("uci: pool size %d", cfg.Size)
	case cfg.Depth < 1:
		return nil, fmt.Errorf("uci: depth %d", cfg.Depth)
	case cfg.Timeout <= 0:
		return nil, fmt.Errorf("uci: position timeout %s", cfg.Timeout)
	case cfg.Start == nil:
		return nil, errors.New("uci: no engine starter")
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}
	if cfg.MaxBackoff < cfg.Backoff {
		cfg.MaxBackoff = 30 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	p := &Pool{cfg: cfg, ctx: ctx, idle: make(chan *Engine, cfg.Size)}
	for range cfg.Size {
		engine, err := cfg.Start(ctx)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.name = engine.Name()
		p.alive.Add(1)
		p.idle <- engine
	}
	return p, nil
}

func (p *Pool) Name() string { return p.name }

func (p *Pool) Depth() int { return p.cfg.Depth }

func (p *Pool) Size() int { return p.cfg.Size }

func (p *Pool) Alive() int { return int(p.alive.Load()) }

func (p *Pool) Check(context.Context) error {
	if p.Alive() == 0 {
		return ErrNoEngines
	}
	return nil
}

func (p *Pool) Evaluate(ctx context.Context, moves []string) (Result, error) {
	result, err := p.attempt(ctx, moves)
	if errors.Is(err, ErrExited) && ctx.Err() == nil {
		p.cfg.Log.WarnContext(ctx, "engine exited during a search, retrying the position", "ply", len(moves))
		return p.attempt(ctx, moves)
	}
	return result, err
}

func (p *Pool) attempt(ctx context.Context, moves []string) (Result, error) {
	var engine *Engine
	select {
	case engine = <-p.idle:
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	search, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	result, err := engine.Analyse(search, moves, p.cfg.Depth)
	timedOut := errors.Is(search.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	p.release(engine)
	if err != nil && timedOut {
		return Result{}, fmt.Errorf("%w after %s at ply %d: %w", ErrTimeout, p.cfg.Timeout, len(moves), err)
	}
	return result, err
}

func (p *Pool) release(engine *Engine) {
	if !engine.Broken() {
		p.idle <- engine
		return
	}
	p.alive.Add(-1)
	p.pending.Go(func() { p.replace(engine) })
}

func (p *Pool) replace(broken *Engine) {
	_ = broken.Close()
	delay := p.cfg.Backoff
	for p.ctx.Err() == nil {
		engine, err := p.cfg.Start(p.ctx)
		if err == nil {
			p.alive.Add(1)
			if p.cfg.OnRestart != nil {
				p.cfg.OnRestart()
			}
			p.cfg.Log.Warn("engine restarted", "alive", p.Alive())
			p.idle <- engine
			return
		}
		p.cfg.Log.Error("engine restart failed", "error", err, "retry_in", delay)
		select {
		case <-p.ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, p.cfg.MaxBackoff)
	}
}

func (p *Pool) Close() {
	p.pending.Wait()
	for {
		select {
		case engine := <-p.idle:
			_ = engine.Close()
		default:
			return
		}
	}
}

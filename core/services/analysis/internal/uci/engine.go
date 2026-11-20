package uci

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	ErrExited       = errors.New("uci: engine exited")
	ErrNoScore      = errors.New("uci: search finished without a score")
	ErrInvalidMove  = errors.New("uci: invalid move")
	ErrUnresponsive = errors.New("uci: engine did not answer stop")
	ErrTimeout      = errors.New("uci: position search timed out")
)

const (
	StopGrace        = 2 * time.Second
	HandshakeTimeout = 15 * time.Second
	maxLine          = 1 << 20
)

type Options struct {
	Threads int
	HashMB  int
}

type Result struct {
	Depth    int
	Score    Score
	BestMove string
}

type Engine struct {
	name     string
	in       io.Writer
	lines    <-chan string
	exited   <-chan struct{}
	quit     chan struct{}
	quitOnce sync.Once
	drained  chan struct{}
	kill     func()
	grace    time.Duration
	broken   bool
}

func Start(ctx context.Context, path string, opts Options) (*Engine, error) {
	cmd := exec.CommandContext(ctx, path)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("uci: stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("uci: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("uci: start %s: %w", path, err)
	}
	exited := make(chan struct{})
	engine := attach(stdin, stdout, func() { _ = cmd.Process.Kill() }, exited)
	go func() {
		<-engine.drained
		_ = cmd.Wait()
		close(exited)
	}()
	handshake, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()
	if err := engine.handshake(handshake, opts); err != nil {
		_ = engine.Close()
		return nil, err
	}
	return engine, nil
}

func Connect(ctx context.Context, conn io.ReadWriteCloser, opts Options) (*Engine, error) {
	exited := make(chan struct{})
	engine := attach(conn, conn, func() { _ = conn.Close() }, exited)
	go func() {
		<-engine.drained
		close(exited)
	}()
	handshake, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()
	if err := engine.handshake(handshake, opts); err != nil {
		_ = engine.Close()
		return nil, err
	}
	return engine, nil
}

func attach(in io.Writer, out io.Reader, kill func(), exited <-chan struct{}) *Engine {
	lines := make(chan string, 256)
	engine := &Engine{in: in, lines: lines, exited: exited, quit: make(chan struct{}), drained: make(chan struct{}), kill: kill, grace: StopGrace}
	go engine.read(out, lines)
	return engine
}

func (e *Engine) read(out io.Reader, lines chan<- string) {
	defer close(e.drained)
	defer close(lines)
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	for scanner.Scan() {
		select {
		case lines <- strings.TrimSpace(scanner.Text()):
		case <-e.quit:
		}
	}
}

func (e *Engine) Name() string { return e.name }

func (e *Engine) Broken() bool { return e.broken }

func (e *Engine) send(commands ...string) error {
	if _, err := io.WriteString(e.in, strings.Join(commands, "\n")+"\n"); err != nil {
		e.broken = true
		return fmt.Errorf("%w: %w", ErrExited, err)
	}
	return nil
}

func (e *Engine) expect(ctx context.Context, want string, seen func(string)) error {
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				e.broken = true
				return ErrExited
			}
			if line == want {
				return nil
			}
			if seen != nil {
				seen(line)
			}
		case <-ctx.Done():
			e.broken = true
			return ctx.Err()
		}
	}
}

func (e *Engine) handshake(ctx context.Context, opts Options) error {
	if err := e.send("uci"); err != nil {
		return err
	}
	if err := e.expect(ctx, "uciok", func(line string) {
		if name, ok := ParseName(line); ok {
			e.name = name
		}
	}); err != nil {
		return fmt.Errorf("uci: handshake: %w", err)
	}
	var setup []string
	if opts.Threads > 0 {
		setup = append(setup, fmt.Sprintf("setoption name Threads value %d", opts.Threads))
	}
	if opts.HashMB > 0 {
		setup = append(setup, fmt.Sprintf("setoption name Hash value %d", opts.HashMB))
	}
	if err := e.send(append(setup, "isready")...); err != nil {
		return err
	}
	if err := e.expect(ctx, "readyok", nil); err != nil {
		return fmt.Errorf("uci: options: %w", err)
	}
	return nil
}

func (e *Engine) Analyse(ctx context.Context, moves []string, depth int) (Result, error) {
	if e.broken {
		return Result{}, ErrExited
	}
	if depth < 1 {
		return Result{}, fmt.Errorf("uci: depth %d", depth)
	}
	for _, move := range moves {
		if !ValidMove(move) {
			return Result{}, fmt.Errorf("%w: %q", ErrInvalidMove, move)
		}
	}
	if err := e.send("ucinewgame", "isready"); err != nil {
		return Result{}, err
	}
	if err := e.expect(ctx, "readyok", nil); err != nil {
		return Result{}, err
	}
	position := "position startpos"
	if len(moves) > 0 {
		position += " moves " + strings.Join(moves, " ")
	}
	if err := e.send(position, fmt.Sprintf("go depth %d", depth)); err != nil {
		return Result{}, err
	}
	var result Result
	scored := false
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				e.broken = true
				return Result{}, ErrExited
			}
			if info, ok := ParseInfo(line); ok {
				if info.HasScore && info.MultiPV <= 1 && (!info.Bound || !scored) {
					result.Score, result.Depth, scored = info.Score, info.Depth, true
				}
				continue
			}
			if best, ok := ParseBestMove(line); ok {
				if !scored {
					return Result{}, ErrNoScore
				}
				result.BestMove = best
				return result, nil
			}
		case <-ctx.Done():
			return Result{}, e.abort(ctx.Err())
		}
	}
}

func (e *Engine) abort(cause error) error {
	if err := e.send("stop"); err != nil {
		return cause
	}
	grace := time.NewTimer(e.grace)
	defer grace.Stop()
	for {
		select {
		case line, ok := <-e.lines:
			if !ok {
				e.broken = true
				return cause
			}
			if _, ok := ParseBestMove(line); ok {
				return cause
			}
		case <-grace.C:
			e.broken = true
			return fmt.Errorf("%w: %w", ErrUnresponsive, cause)
		}
	}
}

func (e *Engine) Close() error {
	e.broken = true
	_, _ = io.WriteString(e.in, "quit\n")
	e.quitOnce.Do(func() { close(e.quit) })
	select {
	case <-e.exited:
		return nil
	case <-time.After(time.Second):
	}
	e.kill()
	<-e.exited
	return nil
}

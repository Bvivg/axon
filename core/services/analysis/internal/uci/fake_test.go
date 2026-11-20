package uci

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var scholar = []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}

type script struct {
	searches [][]string
	hang     bool
	deaf     bool
	die      bool
	silent   bool
	delay    time.Duration
	running  *atomic.Int32
	peak     *atomic.Int32
}

type transcript struct {
	mu   sync.Mutex
	sent []string
}

func (l *transcript) add(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sent = append(l.sent, line)
}

func (l *transcript) has(line string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, sent := range l.sent {
		if sent == line {
			return true
		}
	}
	return false
}

func searchFor(searches [][]string, position string) []string {
	for i := range scholar {
		if position == "position startpos moves "+strings.Join(scholar[:i+1], " ") {
			switch i {
			case 4:
				return searches[0]
			case 5:
				return searches[1]
			case 6:
				return searches[2]
			}
		}
	}
	return searches[5]
}

func fake(t *testing.T, s script) (*Engine, *transcript) {
	t.Helper()
	if s.searches == nil {
		s.searches = recorded(t)
	}
	engine, sent, err := startFake(t.Context(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	return engine, sent
}

func startFake(ctx context.Context, s script) (*Engine, *transcript, error) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	exited := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { _ = outW.Close(); _ = inR.Close() }) }
	engine := attach(inW, outR, stop, exited)
	go func() {
		<-engine.drained
		close(exited)
	}()
	sent := &transcript{}
	go func() {
		defer stop()
		write := func(lines ...string) {
			for _, line := range lines {
				if _, err := fmt.Fprintln(outW, line); err != nil {
					return
				}
			}
		}
		position, searching := "", false
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			line := scanner.Text()
			sent.add(line)
			switch {
			case line == "uci":
				write("Fakefish 1 by tests", "id name Fakefish 1", "id author tests", "option name Hash type spin default 16 min 1 max 1024", "uciok")
			case line == "isready":
				write("readyok")
			case strings.HasPrefix(line, "position "):
				position = line
			case strings.HasPrefix(line, "go "):
				switch {
				case s.die:
					return
				case s.hang:
					searching = true
				case s.silent:
					write("bestmove e2e4")
				default:
					if s.running != nil {
						now := s.running.Add(1)
						for peak := s.peak.Load(); now > peak && !s.peak.CompareAndSwap(peak, now); peak = s.peak.Load() {
						}
					}
					time.Sleep(s.delay)
					if s.running != nil {
						s.running.Add(-1)
					}
					write(searchFor(s.searches, position)...)
				}
			case line == "stop":
				if searching && !s.deaf {
					searching = false
					write("info depth 3 score cp 12", "bestmove e2e4")
				}
			case line == "quit":
				return
			}
		}
	}()
	handshake, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := engine.handshake(handshake, Options{Threads: 1, HashMB: 16}); err != nil {
		_ = engine.Close()
		return nil, nil, err
	}
	return engine, sent, nil
}

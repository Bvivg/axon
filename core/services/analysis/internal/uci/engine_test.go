package uci

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEngineHandshakeAndSearches(t *testing.T) {
	engine, sent := fake(t, script{})
	if engine.Name() != "Fakefish 1" {
		t.Fatalf("name = %q", engine.Name())
	}
	for _, line := range []string{"uci", "setoption name Threads value 1", "setoption name Hash value 16", "isready"} {
		if !sent.has(line) {
			t.Fatalf("handshake did not send %q", line)
		}
	}
	tests := []struct {
		ply  int
		want Result
	}{
		{5, Result{Depth: 12, Score: Score{Value: 38}, BestMove: "g7g6"}},
		{6, Result{Depth: 12, Score: Score{Mate: true, Value: 1}, BestMove: "h5f7"}},
		{7, Result{Depth: 0, Score: Score{Mate: true}}},
		{0, Result{Depth: 12, Score: Score{Value: 38}, BestMove: "b5a4"}},
	}
	for _, tt := range tests {
		got, err := engine.Analyse(t.Context(), scholar[:tt.ply], 12)
		if err != nil {
			t.Fatal(tt.ply, err)
		}
		if got != tt.want {
			t.Fatalf("ply %d = %+v, want %+v", tt.ply, got, tt.want)
		}
	}
	for _, line := range []string{"ucinewgame", "position startpos", "position startpos moves " + strings.Join(scholar, " "), "go depth 12"} {
		if !sent.has(line) {
			t.Fatalf("search did not send %q", line)
		}
	}
}

func TestEngineRefusesMovesThatAreNotUCI(t *testing.T) {
	engine, sent := fake(t, script{})
	if _, err := engine.Analyse(t.Context(), []string{"e2e4", "e7e5\ngo infinite"}, 12); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("err = %v", err)
	}
	if _, err := engine.Analyse(t.Context(), nil, 0); err == nil {
		t.Fatal("depth 0 was accepted")
	}
	if engine.Broken() || sent.has("ucinewgame") {
		t.Fatal("a refused search reached the engine")
	}
}

func TestEngineStopsASearchAtTheDeadline(t *testing.T) {
	engine, sent := fake(t, script{hang: true})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := engine.Analyse(ctx, scholar[:2], 12); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if !sent.has("stop") || engine.Broken() {
		t.Fatalf("stop sent %v, broken %v", sent.has("stop"), engine.Broken())
	}
}

func TestEngineThatIgnoresStopIsBroken(t *testing.T) {
	engine, _ := fake(t, script{hang: true, deaf: true})
	engine.grace = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := engine.Analyse(ctx, nil, 12); !errors.Is(err, ErrUnresponsive) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if !engine.Broken() {
		t.Fatal("an engine that ignored stop is still in use")
	}
	if _, err := engine.Analyse(t.Context(), nil, 12); !errors.Is(err, ErrExited) {
		t.Fatalf("a broken engine searched again: %v", err)
	}
}

func TestEngineThatExitsIsBroken(t *testing.T) {
	engine, _ := fake(t, script{die: true})
	if _, err := engine.Analyse(t.Context(), nil, 12); !errors.Is(err, ErrExited) {
		t.Fatalf("err = %v", err)
	}
	if !engine.Broken() {
		t.Fatal("an exited engine is not broken")
	}
}

func TestEngineWithoutAScore(t *testing.T) {
	engine, _ := fake(t, script{silent: true})
	if _, err := engine.Analyse(t.Context(), nil, 12); !errors.Is(err, ErrNoScore) {
		t.Fatalf("err = %v", err)
	}
	if engine.Broken() {
		t.Fatal("a missing score breaks a healthy engine")
	}
}

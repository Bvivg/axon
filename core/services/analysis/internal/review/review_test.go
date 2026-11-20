package review

import (
	"errors"
	"testing"

	"github.com/bvivg/axon/core/services/analysis/internal/uci"
)

func TestCentipawns(t *testing.T) {
	tests := []struct {
		name  string
		score uci.Score
		want  int
	}{
		{"centipawns", uci.Score{Value: 38}, 38},
		{"negative centipawns", uci.Score{Value: -552}, -552},
		{"mate for the side to move", uci.Score{Mate: true, Value: 1}, MateScore},
		{"mate against the side to move", uci.Score{Mate: true, Value: -2}, -MateScore},
		{"checkmated", uci.Score{Mate: true}, -MateScore},
		{"stalemate", uci.Score{}, 0},
	}
	for _, tt := range tests {
		if got := Centipawns(tt.score); got != tt.want {
			t.Errorf("%s: Centipawns() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestForWhite(t *testing.T) {
	tests := []struct{ ply, value, want int }{{0, 30, 30}, {1, 30, -30}, {2, -45, -45}, {7, -MateScore, MateScore}, {5, 0, 0}}
	for _, tt := range tests {
		if got := ForWhite(tt.ply, tt.value); got != tt.want {
			t.Errorf("ForWhite(%d, %d) = %d, want %d", tt.ply, tt.value, got, tt.want)
		}
	}
}

func TestLoss(t *testing.T) {
	tests := []struct {
		name                 string
		before, after, mover int
		want                 int
	}{
		{"white keeps the evaluation", 30, 30, 0, 0},
		{"white improves", 30, 80, 0, 0},
		{"white drops a pawn", 30, -70, 0, 100},
		{"black drops a piece", -20, 280, 1, 300},
		{"black improves", 40, -60, 1, 0},
		{"black allows mate", -38, MateScore, 1, MateScore + 38},
		{"white keeps a forced mate", MateScore, MateScore, 0, 0},
		{"white throws away a mate", MateScore, 150, 0, MateScore - 150},
	}
	for _, tt := range tests {
		if got := Loss(tt.before, tt.after, tt.mover); got != tt.want {
			t.Errorf("%s: Loss() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name         string
		played, best string
		loss         int
		want         Class
	}{
		{"engine move", "e2e4", "e2e4", 0, Best},
		{"engine move with search noise", "g7g6", "g7g6", 120, Best},
		{"small loss", "d2d4", "e2e4", 12, Good},
		{"just under an inaccuracy", "d2d4", "e2e4", 49, Good},
		{"inaccuracy", "d2d4", "e2e4", 50, Inaccuracy},
		{"just under a mistake", "d2d4", "e2e4", 99, Inaccuracy},
		{"mistake", "d2d4", "e2e4", 100, Mistake},
		{"just under a blunder", "d2d4", "e2e4", 299, Mistake},
		{"blunder", "d2d4", "e2e4", 300, Blunder},
		{"mate allowed", "g8f6", "g7g6", MateScore + 38, Blunder},
		{"no engine move in a final position", "h5f7", "", 0, Good},
	}
	for _, tt := range tests {
		if got := Classify(tt.played, tt.best, tt.loss); got != tt.want {
			t.Errorf("%s: Classify() = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestBuildScholarsMate(t *testing.T) {
	moves := []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}
	results := []uci.Result{
		{Score: uci.Score{Value: 25}, BestMove: "e2e4"},
		{Score: uci.Score{Value: -30}, BestMove: "e7e5"},
		{Score: uci.Score{Value: 35}, BestMove: "g1f3"},
		{Score: uci.Score{Value: -20}, BestMove: "g8f6"},
		{Score: uci.Score{Value: 60}, BestMove: "g1f3"},
		{Score: uci.Score{Value: 38}, BestMove: "g7g6"},
		{Score: uci.Score{Mate: true, Value: 1}, BestMove: "h5f7"},
		{Score: uci.Score{Mate: true}},
	}
	positions, err := Build(moves, results)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		cp    int
		mate  *int
		loss  int
		class Class
	}{
		{25, nil, 0, ClassNone},
		{30, nil, 0, Best},
		{35, nil, 5, Best},
		{20, nil, 15, Good},
		{60, nil, 40, Good},
		{-38, nil, 98, Inaccuracy},
		{MateScore, new(1), MateScore + 38, Blunder},
		{MateScore, new(0), 0, Best},
	}
	if len(positions) != len(want) {
		t.Fatalf("%d positions", len(positions))
	}
	for ply, p := range positions {
		w := want[ply]
		if p.Ply != ply || p.Centipawns != w.cp || p.Loss != w.loss || p.Class != w.class || (p.Mate == nil) != (w.mate == nil) || (p.Mate != nil && *p.Mate != *w.mate) {
			t.Errorf("ply %d = %+v (mate %v), want %+v", ply, p, p.Mate, w)
		}
		if ply > 0 && p.Played != moves[ply-1] {
			t.Errorf("ply %d played %q", ply, p.Played)
		}
	}
	if positions[0].Played != "" || positions[7].BestMove != "" {
		t.Fatalf("start %+v, end %+v", positions[0], positions[7])
	}
}

func TestBuildRefusesMismatchedInput(t *testing.T) {
	if _, err := Build([]string{"e2e4"}, []uci.Result{{}}); !errors.Is(err, ErrMismatch) {
		t.Fatalf("err = %v", err)
	}
	positions, err := Build(nil, []uci.Result{{Score: uci.Score{Value: 20}, BestMove: "e2e4"}})
	if err != nil || len(positions) != 1 || positions[0].Class != ClassNone {
		t.Fatalf("start only = %+v, %v", positions, err)
	}
}

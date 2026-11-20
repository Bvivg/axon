package analyser_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/bvivg/axon/core/services/analysis/internal/analyser"
	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/uci"
)

type table struct {
	mu      sync.Mutex
	results map[string]uci.Result
	fail    map[int]error
	asked   []int
}

func (t *table) Evaluate(ctx context.Context, moves []string) (uci.Result, error) {
	t.mu.Lock()
	t.asked = append(t.asked, len(moves))
	err := t.fail[len(moves)]
	t.mu.Unlock()
	if err != nil {
		return uci.Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return uci.Result{}, err
	}
	return t.results[strings.Join(moves, " ")], nil
}

var scholar = []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}

func scholarTable() *table {
	scores := []uci.Result{
		{Depth: 12, Score: uci.Score{Value: 25}, BestMove: "e2e4"},
		{Depth: 12, Score: uci.Score{Value: -30}, BestMove: "e7e5"},
		{Depth: 12, Score: uci.Score{Value: 35}, BestMove: "g1f3"},
		{Depth: 12, Score: uci.Score{Value: -20}, BestMove: "g8f6"},
		{Depth: 12, Score: uci.Score{Value: 60}, BestMove: "g1f3"},
		{Depth: 12, Score: uci.Score{Value: 38}, BestMove: "g7g6"},
		{Depth: 12, Score: uci.Score{Mate: true, Value: 1}, BestMove: "h5f7"},
		{Score: uci.Score{Mate: true}},
	}
	t := &table{results: map[string]uci.Result{}, fail: map[int]error{}}
	for ply, result := range scores {
		t.results[strings.Join(scholar[:ply], " ")] = result
	}
	return t
}

func TestAnalyseEvaluatesEveryPosition(t *testing.T) {
	engines := scholarTable()
	positions, err := analyser.New(engines).Analyse(t.Context(), scholar)
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != len(scholar)+1 || len(engines.asked) != len(scholar)+1 {
		t.Fatalf("%d positions from %d evaluations", len(positions), len(engines.asked))
	}
	if positions[6].Class != review.Blunder || positions[6].Played != "g8f6" || positions[7].Mate == nil || *positions[7].Mate != 0 {
		t.Fatalf("ply 6 %+v, ply 7 %+v", positions[6], positions[7])
	}
}

func TestAnalyseFailsWhenAPositionFails(t *testing.T) {
	engines := scholarTable()
	engines.fail[3] = uci.ErrTimeout
	if _, err := analyser.New(engines).Analyse(t.Context(), scholar); !errors.Is(err, uci.ErrTimeout) || !strings.Contains(err.Error(), "ply 3") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyseRefusesMovesThatAreNotUCI(t *testing.T) {
	engines := scholarTable()
	if _, err := analyser.New(engines).Analyse(t.Context(), []string{"e2e4", "Nf6"}); !errors.Is(err, uci.ErrInvalidMove) {
		t.Fatalf("err = %v", err)
	}
	if len(engines.asked) != 0 {
		t.Fatal("an invalid game reached the engines")
	}
}

func TestAnalyseTheStartPositionOnly(t *testing.T) {
	positions, err := analyser.New(scholarTable()).Analyse(t.Context(), nil)
	if err != nil || len(positions) != 1 || positions[0].BestMove != "e2e4" {
		t.Fatalf("positions = %+v, %v", positions, err)
	}
}

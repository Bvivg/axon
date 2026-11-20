package analyser

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/bvivg/axon/core/services/analysis/internal/review"
	"github.com/bvivg/axon/core/services/analysis/internal/uci"
)

type Evaluator interface {
	Evaluate(ctx context.Context, moves []string) (uci.Result, error)
}

type Analyser struct {
	engines Evaluator
}

func New(engines Evaluator) *Analyser { return &Analyser{engines: engines} }

func (a *Analyser) Analyse(ctx context.Context, moves []string) ([]review.Position, error) {
	for ply, move := range moves {
		if !uci.ValidMove(move) {
			return nil, fmt.Errorf("analyser: move %d: %w: %q", ply+1, uci.ErrInvalidMove, move)
		}
	}
	results := make([]uci.Result, len(moves)+1)
	group, search := errgroup.WithContext(ctx)
	for ply := range results {
		group.Go(func() error {
			result, err := a.engines.Evaluate(search, moves[:ply])
			if err != nil {
				return fmt.Errorf("analyser: ply %d: %w", ply, err)
			}
			results[ply] = result
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return review.Build(moves, results)
}

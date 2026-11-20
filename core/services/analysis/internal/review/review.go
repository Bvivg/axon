package review

import (
	"errors"
	"fmt"

	"github.com/bvivg/axon/core/services/analysis/internal/uci"
)

type Class string

const (
	ClassNone  Class = "none"
	Best       Class = "best"
	Good       Class = "good"
	Inaccuracy Class = "inaccuracy"
	Mistake    Class = "mistake"
	Blunder    Class = "blunder"
)

const (
	MateScore      = 10000
	InaccuracyLoss = 50
	MistakeLoss    = 100
	BlunderLoss    = 300
)

var ErrMismatch = errors.New("review: evaluations do not match the moves")

type Position struct {
	Ply        int
	Centipawns int
	Mate       *int
	BestMove   string
	Played     string
	Loss       int
	Class      Class
}

func Centipawns(score uci.Score) int {
	switch {
	case !score.Mate:
		return score.Value
	case score.Value > 0:
		return MateScore
	default:
		return -MateScore
	}
}

func whiteToMove(ply int) bool { return ply%2 == 0 }

func ForWhite(ply, value int) int {
	if whiteToMove(ply) {
		return value
	}
	return -value
}

func Loss(before, after, mover int) int {
	if mover == 1 {
		before, after = -before, -after
	}
	return max(0, before-after)
}

func Classify(played, best string, loss int) Class {
	switch {
	case played != "" && played == best:
		return Best
	case loss >= BlunderLoss:
		return Blunder
	case loss >= MistakeLoss:
		return Mistake
	case loss >= InaccuracyLoss:
		return Inaccuracy
	default:
		return Good
	}
}

func evaluated(ply int, result uci.Result) Position {
	position := Position{Ply: ply, Centipawns: ForWhite(ply, Centipawns(result.Score)), BestMove: result.BestMove, Class: ClassNone}
	if result.Score.Mate {
		position.Mate = new(ForWhite(ply, result.Score.Value))
	}
	return position
}

func Build(moves []string, results []uci.Result) ([]Position, error) {
	if len(results) != len(moves)+1 {
		return nil, fmt.Errorf("%w: %d moves, %d evaluations", ErrMismatch, len(moves), len(results))
	}
	previous := evaluated(0, results[0])
	positions := append(make([]Position, 0, len(results)), previous)
	for i, result := range results[1:] {
		position := evaluated(i+1, result)
		position.Played = moves[i]
		position.Loss = Loss(previous.Centipawns, position.Centipawns, i%2)
		position.Class = Classify(position.Played, previous.BestMove, position.Loss)
		positions = append(positions, position)
		previous = position
	}
	return positions, nil
}

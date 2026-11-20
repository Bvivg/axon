package tictactoe

import "github.com/bvivg/axon/core/services/game/internal/engine"

const Size = 3

const emptyCell = -1

type board struct {
	Cells [Size * Size]int `json:"cells"`
}

func newBoard() board {
	var b board
	for i := range b.Cells {
		b.Cells[i] = emptyCell
	}
	return b
}

func index(row, col int) int { return row*Size + col }

func onBoard(row, col int) bool {
	return row >= 0 && row < Size && col >= 0 && col < Size
}

var winningLines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

func (b board) winningLine(seat int) []int {
	for _, line := range winningLines {
		if b.Cells[line[0]] == seat && b.Cells[line[1]] == seat && b.Cells[line[2]] == seat {
			return line[:]
		}
	}
	return nil
}

func Position(state engine.State) ([Size * Size]int, []int, error) {
	var b board
	if err := engine.DecodeState(state, &b); err != nil {
		return b.Cells, nil, err
	}
	if state.Status == engine.StatusWin {
		return b.Cells, b.winningLine(state.Winner), nil
	}
	return b.Cells, nil, nil
}

func (b board) full() bool {
	for _, cell := range b.Cells {
		if cell == emptyCell {
			return false
		}
	}
	return true
}

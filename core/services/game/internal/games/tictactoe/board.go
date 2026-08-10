package tictactoe

// Size is the side of the board.
const Size = 3

// emptyCell marks a cell nobody has taken. Taken cells hold the seat index of
// their owner rather than a symbol: "X" and "O" are a rendering decision, and
// keeping seats in the payload means the stored game says who played where
// without a second lookup.
const emptyCell = -1

// board is the game-specific payload of a position — everything engine.State
// deliberately does not know.
//
// Its JSON shape is a stored contract: it lands in the sessions cache and in the
// moves table, and the web client renders it, so field names change only with a
// migration.
type board struct {
	Cells [Size * Size]int `json:"cells"`
}

// newBoard returns an empty board.
func newBoard() board {
	var b board
	for i := range b.Cells {
		b.Cells[i] = emptyCell
	}
	return b
}

// index turns a row and column into an offset into Cells.
func index(row, col int) int { return row*Size + col }

// onBoard reports whether a row and column name a real cell.
func onBoard(row, col int) bool {
	return row >= 0 && row < Size && col >= 0 && col < Size
}

// winningLines are the eight ways to win. Spelled out rather than generated:
// three-in-a-row on a 3x3 board is a closed set, and a loop that computes it
// would be longer than the answer.
var winningLines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

// wins reports whether the given seat holds a full line.
func (b board) wins(seat int) bool {
	for _, line := range winningLines {
		if b.Cells[line[0]] == seat && b.Cells[line[1]] == seat && b.Cells[line[2]] == seat {
			return true
		}
	}
	return false
}

// full reports whether every cell is taken.
func (b board) full() bool {
	for _, cell := range b.Cells {
		if cell == emptyCell {
			return false
		}
	}
	return true
}

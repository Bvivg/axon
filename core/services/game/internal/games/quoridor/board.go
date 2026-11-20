package quoridor

import (
	"errors"
	"fmt"
)

const (
	Size      = 9
	WallsEach = 10
)

var ErrBadMove = errors.New("quoridor: malformed move")

type Cell int

func (c Cell) Row() int { return int(c) / Size }
func (c Cell) Col() int { return int(c) % Size }

func At(row, col int) Cell { return Cell(row*Size + col) }

func (c Cell) String() string {
	return fmt.Sprintf("%c%d", 'a'+c.Col(), c.Row()+1)
}

type Orientation byte

const (
	Horizontal Orientation = 'h'
	Vertical   Orientation = 'v'
)

type Wall struct {
	Anchor Cell
	Dir    Orientation
}

func (w Wall) String() string { return w.Anchor.String() + string(w.Dir) }

type Move struct {
	Wall *Wall
	To   Cell
}

func (m Move) String() string {
	if m.Wall != nil {
		return m.Wall.String()
	}
	return m.To.String()
}

func ParseMove(raw string) (Move, error) {
	if len(raw) < 2 || len(raw) > 3 {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	col, row := int(raw[0]-'a'), int(raw[1]-'1')
	if col < 0 || col >= Size || row < 0 || row >= Size {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	if len(raw) == 2 {
		return Move{To: At(row, col)}, nil
	}
	dir := Orientation(raw[2])
	if (dir != Horizontal && dir != Vertical) || col >= Size-1 || row >= Size-1 {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	return Move{Wall: &Wall{Anchor: At(row, col), Dir: dir}}, nil
}

type Board struct {
	Pawns [2]Cell
	Left  [2]int
	walls map[Wall]bool
}

func NewBoard() Board {
	return Board{Pawns: [2]Cell{At(0, 4), At(Size-1, 4)}, Left: [2]int{WallsEach, WallsEach}, walls: map[Wall]bool{}}
}

func (b Board) Walls() []Wall {
	out := make([]Wall, 0, len(b.walls))
	for w := range b.walls {
		out = append(out, w)
	}
	return out
}

func (b Board) HasWall(w Wall) bool { return b.walls[w] }

func (b Board) with(w Wall) Board {
	next := b
	next.walls = make(map[Wall]bool, len(b.walls)+1)
	for k := range b.walls {
		next.walls[k] = true
	}
	next.walls[w] = true
	return next
}

func GoalRow(seat int) int {
	if seat == 0 {
		return Size - 1
	}
	return 0
}

func (b Board) Blocked(from, to Cell) bool {
	fr, fc, tr, tc := from.Row(), from.Col(), to.Row(), to.Col()
	switch {
	case fc == tc && abs(fr-tr) == 1:
		row := min(fr, tr)
		return b.walls[Wall{At(row, fc), Horizontal}] || (fc > 0 && b.walls[Wall{At(row, fc-1), Horizontal}])
	case fr == tr && abs(fc-tc) == 1:
		col := min(fc, tc)
		return b.walls[Wall{At(fr, col), Vertical}] || (fr > 0 && b.walls[Wall{At(fr-1, col), Vertical}])
	}
	return true
}

func (b Board) neighbours(c Cell) []Cell {
	out := make([]Cell, 0, 4)
	r, col := c.Row(), c.Col()
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		nr, nc := r+d[0], col+d[1]
		if nr < 0 || nr >= Size || nc < 0 || nc >= Size {
			continue
		}
		next := At(nr, nc)
		if !b.Blocked(c, next) {
			out = append(out, next)
		}
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

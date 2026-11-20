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

const anchors = Size - 1

type Board struct {
	Pawns [2]Cell
	Left  [2]int
	walls [2]uint64
}

func NewBoard() Board {
	return Board{Pawns: [2]Cell{At(0, 4), At(Size-1, 4)}, Left: [2]int{WallsEach, WallsEach}}
}

func orientation(d Orientation) int {
	if d == Vertical {
		return 1
	}
	return 0
}

func bit(row, col int) uint64 { return 1 << uint(row*anchors+col) }

func (b Board) wall(row, col int, d Orientation) bool {
	return b.walls[orientation(d)]&bit(row, col) != 0
}

func (b Board) Walls() []Wall {
	var out []Wall
	for _, d := range [2]Orientation{Horizontal, Vertical} {
		for r := 0; r < anchors; r++ {
			for c := 0; c < anchors; c++ {
				if b.wall(r, c, d) {
					out = append(out, Wall{At(r, c), d})
				}
			}
		}
	}
	return out
}

func (b Board) HasWall(w Wall) bool { return b.wall(w.Anchor.Row(), w.Anchor.Col(), w.Dir) }

func (b Board) with(w Wall) Board {
	b.walls[orientation(w.Dir)] |= bit(w.Anchor.Row(), w.Anchor.Col())
	return b
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
		return b.crossesHorizontal(min(fr, tr), fc)
	case fr == tr && abs(fc-tc) == 1:
		return b.crossesVertical(fr, min(fc, tc))
	}
	return true
}

func (b Board) crossesHorizontal(row, col int) bool {
	return (col < anchors && b.wall(row, col, Horizontal)) || (col > 0 && b.wall(row, col-1, Horizontal))
}

func (b Board) crossesVertical(row, col int) bool {
	return (row < anchors && b.wall(row, col, Vertical)) || (row > 0 && b.wall(row-1, col, Vertical))
}

var directions = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

func (b Board) step(c Cell, dir int) (Cell, bool) {
	nr, nc := c.Row()+directions[dir][0], c.Col()+directions[dir][1]
	if nr < 0 || nr >= Size || nc < 0 || nc >= Size {
		return 0, false
	}
	next := At(nr, nc)
	return next, !b.Blocked(c, next)
}

func (b Board) neighbours(c Cell) []Cell {
	out := make([]Cell, 0, 4)
	for dir := range directions {
		if next, ok := b.step(c, dir); ok {
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

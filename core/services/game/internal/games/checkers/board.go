package checkers

import (
	"errors"
	"fmt"
)

const (
	Squares   = 32
	Size      = 8
	MenEach   = 12
	Black     = 0
	White     = 1
	noSquare  = -1
	emptyMark = '.'
)

var ErrBadBoard = errors.New("checkers: malformed board")

type Piece byte

const (
	Empty     Piece = emptyMark
	BlackMan  Piece = 'b'
	BlackKing Piece = 'B'
	WhiteMan  Piece = 'w'
	WhiteKing Piece = 'W'
)

func (p Piece) Side() int {
	switch p {
	case BlackMan, BlackKing:
		return Black
	case WhiteMan, WhiteKing:
		return White
	}
	return noSquare
}

func (p Piece) King() bool { return p == BlackKing || p == WhiteKing }

func (p Piece) crowned() Piece {
	switch p {
	case BlackMan:
		return BlackKing
	case WhiteMan:
		return WhiteKing
	}
	return p
}

type Square int

func (s Square) Row() int { return int(s) / 4 }

func (s Square) Col() int { return 2*(int(s)%4) + 1 - s.Row()%2 }

func (s Square) Number() int { return int(s) + 1 }

func At(row, col int) (Square, bool) {
	if row < 0 || row >= Size || col < 0 || col >= Size || (row+col)%2 == 0 {
		return noSquare, false
	}
	return Square(row*4 + col/2), true
}

type Board [Squares]Piece

func NewBoard() Board {
	var b Board
	for s := range b {
		switch {
		case s < MenEach:
			b[s] = BlackMan
		case s >= Squares-MenEach:
			b[s] = WhiteMan
		default:
			b[s] = Empty
		}
	}
	return b
}

func ParseBoard(raw string) (Board, error) {
	var b Board
	if len(raw) != Squares {
		return Board{}, fmt.Errorf("%w: %d squares", ErrBadBoard, len(raw))
	}
	for i := range Squares {
		switch p := Piece(raw[i]); p {
		case Empty, BlackMan, BlackKing, WhiteMan, WhiteKing:
			b[i] = p
		default:
			return Board{}, fmt.Errorf("%w: square %d holds %q", ErrBadBoard, i+1, raw[i])
		}
	}
	return b, nil
}

func (b Board) String() string {
	out := make([]byte, Squares)
	for i, p := range b {
		out[i] = byte(p)
	}
	return string(out)
}

func (b Board) Count(side int) int {
	n := 0
	for _, p := range b {
		if p.Side() == side {
			n++
		}
	}
	return n
}

var diagonals = [4][2]int{{1, -1}, {1, 1}, {-1, -1}, {-1, 1}}

var neighbours, landings = geometry()

func geometry() (next, jump [Squares][4]Square) {
	for s := range Squares {
		sq := Square(s)
		for d, step := range diagonals {
			next[s][d], jump[s][d] = noSquare, noSquare
			if n, ok := At(sq.Row()+step[0], sq.Col()+step[1]); ok {
				next[s][d] = n
			}
			if j, ok := At(sq.Row()+2*step[0], sq.Col()+2*step[1]); ok {
				jump[s][d] = j
			}
		}
	}
	return next, jump
}

func directions(p Piece) []int {
	switch {
	case p.King():
		return []int{0, 1, 2, 3}
	case p.Side() == Black:
		return []int{0, 1}
	default:
		return []int{2, 3}
	}
}

func crownRow(side int) int {
	if side == Black {
		return Size - 1
	}
	return 0
}

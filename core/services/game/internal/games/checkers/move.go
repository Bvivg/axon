package checkers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	MaxPath     = MenEach + 1
	MaxNotation = MaxPath*3 - 1
)

var ErrBadMove = errors.New("checkers: malformed move")

type Move struct {
	Path []Square
	Jump bool
}

func (m Move) From() Square { return m.Path[0] }

func (m Move) To() Square { return m.Path[len(m.Path)-1] }

func (m Move) String() string {
	sep := "-"
	if m.Jump {
		sep = "x"
	}
	var out strings.Builder
	for i, s := range m.Path {
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(strconv.Itoa(s.Number()))
	}
	return out.String()
}

func ParseMove(raw string) (Move, error) {
	if len(raw) < 3 || len(raw) > MaxNotation {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	jump := strings.IndexByte(raw, 'x') >= 0
	step := strings.IndexByte(raw, '-') >= 0
	if jump == step {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	sep := "-"
	if jump {
		sep = "x"
	}
	parts := strings.Split(raw, sep)
	if len(parts) < 2 || len(parts) > MaxPath || !jump && len(parts) != 2 {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
	path := make([]Square, 0, len(parts))
	for _, part := range parts {
		n, ok := number(part)
		if !ok {
			return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
		}
		path = append(path, Square(n-1))
	}
	return Move{Path: path, Jump: jump}, nil
}

func number(part string) (int, bool) {
	if len(part) == 0 || len(part) > 2 || part[0] == '0' {
		return 0, false
	}
	n := 0
	for i := range len(part) {
		if part[i] < '0' || part[i] > '9' {
			return 0, false
		}
		n = n*10 + int(part[i]-'0')
	}
	return n, n >= 1 && n <= Squares
}

package checkers

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func position(pieces map[int]Piece) Board {
	var b Board
	for s := range b {
		b[s] = Empty
	}
	for number, p := range pieces {
		b[number-1] = p
	}
	return b
}

func notations(moves []Move) []string {
	out := []string{}
	for _, m := range moves {
		out = append(out, m.String())
	}
	return out
}

func sorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}

func perft(b Board, side, depth int) int {
	moves := b.LegalMoves(side)
	if depth == 1 {
		return len(moves)
	}
	n := 0
	for _, m := range moves {
		n += perft(b.Play(m), 1-side, depth-1)
	}
	return n
}

func TestPerft(t *testing.T) {
	want := []int{7, 49, 302, 1469, 7361, 36768, 179740, 845931}
	for i, nodes := range want {
		if got := perft(NewBoard(), Black, i+1); got != nodes {
			t.Fatalf("perft(%d) = %d, want %d", i+1, got, nodes)
		}
	}
}

func TestGeometry(t *testing.T) {
	tests := []struct {
		square   int
		row, col int
	}{
		{1, 0, 1}, {4, 0, 7}, {5, 1, 0}, {8, 1, 6}, {9, 2, 1}, {13, 3, 0}, {29, 7, 0}, {32, 7, 6},
	}
	for _, tt := range tests {
		s := Square(tt.square - 1)
		if s.Row() != tt.row || s.Col() != tt.col {
			t.Errorf("square %d at (%d,%d), want (%d,%d)", tt.square, s.Row(), s.Col(), tt.row, tt.col)
		}
		if back, ok := At(tt.row, tt.col); !ok || back != s {
			t.Errorf("At(%d,%d) = %d, %v", tt.row, tt.col, back, ok)
		}
	}
	if _, ok := At(0, 0); ok {
		t.Error("a light square is playable")
	}
}

func TestLegalMoves(t *testing.T) {
	tests := []struct {
		name  string
		board Board
		side  int
		want  []string
	}{
		{name: "black opening", board: NewBoard(), side: Black, want: []string{"9-13", "9-14", "10-14", "10-15", "11-15", "11-16", "12-16"}},
		{name: "white opening", board: NewBoard(), side: White, want: []string{"21-17", "22-17", "22-18", "23-18", "23-19", "24-19", "24-20"}},
		{
			name:  "capture is forced",
			board: position(map[int]Piece{15: BlackMan, 1: BlackMan, 18: WhiteMan, 32: WhiteMan}),
			side:  Black,
			want:  []string{"15x22"},
		},
		{
			name:  "men never capture backwards",
			board: position(map[int]Piece{15: BlackMan, 11: WhiteMan}),
			side:  Black,
			want:  []string{"15-18", "15-19"},
		},
		{
			name:  "multi-jump continues and any branch may be chosen",
			board: position(map[int]Piece{6: BlackMan, 10: WhiteMan, 18: WhiteMan, 19: WhiteMan, 27: WhiteMan}),
			side:  Black,
			want:  []string{"6x15x22", "6x15x24x31"},
		},
		{
			name:  "crowning ends the jump",
			board: position(map[int]Piece{22: BlackMan, 26: WhiteMan, 27: WhiteMan}),
			side:  Black,
			want:  []string{"22x31"},
		},
		{
			name:  "king steps one square every way",
			board: position(map[int]Piece{18: BlackKing, 1: WhiteMan}),
			side:  Black,
			want:  []string{"18-22", "18-23", "18-14", "18-15"},
		},
		{
			name:  "king captures backwards",
			board: position(map[int]Piece{18: WhiteKing, 22: BlackMan, 1: BlackMan}),
			side:  White,
			want:  []string{"18x25"},
		},
		{
			name:  "kings do not fly",
			board: position(map[int]Piece{1: BlackKing, 32: WhiteMan}),
			side:  Black,
			want:  []string{"1-5", "1-6"},
		},
		{
			name:  "king captures in every direction",
			board: position(map[int]Piece{18: BlackKing, 14: WhiteMan, 15: WhiteMan, 22: WhiteMan, 23: WhiteMan}),
			side:  Black,
			want:  []string{"18x25", "18x27", "18x9", "18x11"},
		},
		{
			name:  "king may circle back to its own square",
			board: position(map[int]Piece{18: BlackKing, 15: WhiteMan, 7: WhiteMan, 6: WhiteMan, 14: WhiteMan}),
			side:  Black,
			want:  []string{"18x11x2x9x18", "18x9x2x11x18"},
		},
		{
			name:  "blocked side has no moves",
			board: position(map[int]Piece{29: WhiteMan, 25: BlackMan, 22: BlackMan}),
			side:  White,
			want:  []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := notations(tt.board.LegalMoves(tt.side)); !reflect.DeepEqual(sorted(got), sorted(tt.want)) {
				t.Errorf("LegalMoves() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlay(t *testing.T) {
	tests := []struct {
		name  string
		board Board
		move  string
		want  Board
	}{
		{name: "step", board: NewBoard(), move: "11-15", want: func() Board { b := NewBoard(); b[10], b[14] = Empty, BlackMan; return b }()},
		{
			name:  "double jump removes both pieces",
			board: position(map[int]Piece{6: BlackMan, 10: WhiteMan, 19: WhiteMan, 32: WhiteMan}),
			move:  "6x15x24",
			want:  position(map[int]Piece{24: BlackMan, 32: WhiteMan}),
		},
		{
			name:  "man is crowned on the last row",
			board: position(map[int]Piece{27: BlackMan, 1: WhiteMan}),
			move:  "27-32",
			want:  position(map[int]Piece{32: BlackKing, 1: WhiteMan}),
		},
		{
			name:  "white is crowned on the first row",
			board: position(map[int]Piece{6: WhiteMan, 32: BlackMan}),
			move:  "6-1",
			want:  position(map[int]Piece{1: WhiteKing, 32: BlackMan}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := ParseMove(tt.move)
			if err != nil {
				t.Fatal(err)
			}
			side := tt.board[m.From()].Side()
			legal, ok := tt.board.Legal(side, m)
			if !ok {
				t.Fatalf("%s is not legal: %v", tt.move, notations(tt.board.LegalMoves(side)))
			}
			if got := tt.board.Play(legal); got != tt.want {
				t.Errorf("Play() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseMove(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{raw: "11-15"},
		{raw: "1-5"},
		{raw: "32-28"},
		{raw: "22x15x6"},
		{raw: "15x22"},
		{raw: "18x11x2x9x18"},
		{raw: "", wantErr: true},
		{raw: "11", wantErr: true},
		{raw: "11-", wantErr: true},
		{raw: "-15", wantErr: true},
		{raw: "11-15-19", wantErr: true},
		{raw: "11x", wantErr: true},
		{raw: "0-4", wantErr: true},
		{raw: "33-29", wantErr: true},
		{raw: "01-05", wantErr: true},
		{raw: "+1-5", wantErr: true},
		{raw: "11-15x19", wantErr: true},
		{raw: "11–15", wantErr: true},
		{raw: "11 -15", wantErr: true},
		{raw: "1x10x1x10x1x10x1x10x1x10x1x10x1x10", wantErr: true},
		{raw: strings.Repeat("1", 64), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			m, err := ParseMove(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrBadMove) {
					t.Fatalf("ParseMove(%q) = %v, %v, want ErrBadMove", tt.raw, m, err)
				}
				return
			}
			if err != nil || m.String() != tt.raw {
				t.Fatalf("ParseMove(%q) round-trips to %q, %v", tt.raw, m.String(), err)
			}
		})
	}
}

func FuzzParseMove(f *testing.F) {
	for _, seed := range []string{"11-15", "22x15x6", "", "x", "-", "1x", "99-99", "1-2-3", "32x23x14x5"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		m, err := ParseMove(raw)
		if err != nil {
			return
		}
		if len(m.Path) < 2 || len(m.Path) > MaxPath {
			t.Fatalf("ParseMove(%q) gave a path of %d squares", raw, len(m.Path))
		}
		again, err := ParseMove(m.String())
		if err != nil || again.String() != m.String() {
			t.Fatalf("ParseMove(%q) does not round-trip: %q, %v", raw, m.String(), err)
		}
	})
}

func TestParseBoard(t *testing.T) {
	start := NewBoard()
	if got, err := ParseBoard(start.String()); err != nil || got != start {
		t.Fatalf("ParseBoard(start) = %v, %v", got, err)
	}
	if start.String() != "bbbbbbbbbbbb........wwwwwwwwwwww" {
		t.Fatalf("start board = %q", start.String())
	}
	for _, bad := range []string{"", strings.Repeat(".", 31), strings.Repeat(".", 33), strings.Repeat(".", 31) + "x"} {
		if _, err := ParseBoard(bad); !errors.Is(err, ErrBadBoard) {
			t.Errorf("ParseBoard(%q) = %v, want ErrBadBoard", bad, err)
		}
	}
}

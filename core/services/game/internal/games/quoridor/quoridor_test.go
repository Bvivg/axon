package quoridor

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

func cell(t *testing.T, raw string) Cell {
	t.Helper()
	m, err := ParseMove(raw)
	if err != nil || m.Wall != nil {
		t.Fatalf("bad cell %q", raw)
	}
	return m.To
}

func wall(t *testing.T, raw string) Wall {
	t.Helper()
	m, err := ParseMove(raw)
	if err != nil || m.Wall == nil {
		t.Fatalf("bad wall %q", raw)
	}
	return *m.Wall
}

func board(t *testing.T, white, black string, walls ...string) Board {
	t.Helper()
	b := NewBoard()
	b.Pawns = [2]Cell{cell(t, white), cell(t, black)}
	for _, w := range walls {
		b = b.with(wall(t, w))
	}
	return b
}

func names(cells []Cell) []string {
	out := []string{}
	for _, c := range cells {
		out = append(out, c.String())
	}
	return out
}

func TestParseMove(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "e2", want: "e2"},
		{raw: "a1", want: "a1"},
		{raw: "i9", want: "i9"},
		{raw: "e3h", want: "e3h"},
		{raw: "h8v", want: "h8v"},
		{raw: "i8h", wantErr: true},
		{raw: "a9v", wantErr: true},
		{raw: "j1", wantErr: true},
		{raw: "e0", wantErr: true},
		{raw: "e3x", wantErr: true},
		{raw: "e", wantErr: true},
		{raw: "e3hh", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			m, err := ParseMove(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrBadMove) {
					t.Fatalf("ParseMove(%q) error = %v, want ErrBadMove", tt.raw, err)
				}
				return
			}
			if err != nil || m.String() != tt.want {
				t.Fatalf("ParseMove(%q) = %v, %v", tt.raw, m, err)
			}
		})
	}
}

func TestPawnMoves(t *testing.T) {
	tests := []struct {
		name  string
		board func(t *testing.T) Board
		seat  int
		want  []string
	}{
		{name: "opening", board: func(t *testing.T) Board { return NewBoard() }, seat: 0, want: []string{"d1", "f1", "e2"}},
		{name: "black opening", board: func(t *testing.T) Board { return NewBoard() }, seat: 1, want: []string{"e8", "d9", "f9"}},
		{name: "straight jump", board: func(t *testing.T) Board { return board(t, "e5", "e6") }, seat: 0, want: []string{"e4", "d5", "f5", "e7"}},
		{name: "wall behind the opponent gives diagonals", board: func(t *testing.T) Board { return board(t, "e5", "e6", "e6h") }, seat: 0, want: []string{"e4", "d5", "f5", "d6", "f6"}},
		{name: "board edge behind the opponent gives diagonals", board: func(t *testing.T) Board { return board(t, "e8", "e9") }, seat: 0, want: []string{"e7", "d8", "f8", "d9", "f9"}},
		{name: "diagonal closed by a wall", board: func(t *testing.T) Board { return board(t, "e5", "e6", "e6h", "d6v") }, seat: 0, want: []string{"e4", "d5", "f5", "f6"}},
		{name: "sideways jump", board: func(t *testing.T) Board { return board(t, "e5", "f5") }, seat: 0, want: []string{"e4", "d5", "g5", "e6"}},
		{name: "wall in front", board: func(t *testing.T) Board { return board(t, "e1", "e9", "e1h") }, seat: 0, want: []string{"d1", "f1"}},
		{name: "wall covers the second cell", board: func(t *testing.T) Board { return board(t, "e1", "e9", "d1h") }, seat: 0, want: []string{"d1", "f1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(tt.board(t).PawnMoves(tt.seat))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PawnMoves() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanPlace(t *testing.T) {
	base := func(t *testing.T) Board { return board(t, "e1", "e9", "e3h") }
	sealed := func(t *testing.T) Board { return board(t, "e1", "e9", "a1h", "c1h", "e1h", "g1h") }
	tests := []struct {
		name  string
		board func(t *testing.T) Board
		wall  string
		want  bool
	}{
		{name: "same place", board: base, wall: "e3h", want: false},
		{name: "overlaps from the left", board: base, wall: "d3h", want: false},
		{name: "overlaps from the right", board: base, wall: "f3h", want: false},
		{name: "crosses", board: base, wall: "e3v", want: false},
		{name: "touches end to end", board: base, wall: "c3h", want: true},
		{name: "touches the other end", board: base, wall: "g3h", want: true},
		{name: "vertical below", board: base, wall: "e2v", want: true},
		{name: "vertical above", board: base, wall: "e4v", want: true},
		{name: "vertical overlaps vertically", board: func(t *testing.T) Board { return board(t, "e1", "e9", "c4v") }, wall: "c5v", want: false},
		{name: "last gap left open", board: sealed, wall: "h2v", want: true},
		{name: "closes the last gap", board: sealed, wall: "h1v", want: false},
		{name: "boxes in the opponent", board: func(t *testing.T) Board { return board(t, "e1", "a9", "a8h") }, wall: "b8v", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.board(t).CanPlace(0, wall(t, tt.wall)); got != tt.want {
				t.Errorf("CanPlace(%s) = %v, want %v", tt.wall, got, tt.want)
			}
		})
	}
	t.Run("no walls left", func(t *testing.T) {
		b := NewBoard()
		b.Left[0] = 0
		if b.CanPlace(0, wall(t, "a1h")) {
			t.Error("placed a wall with none left")
		}
		if b.CanPlace(1, wall(t, "a1h")) != true {
			t.Error("the opponent still has walls")
		}
	})
}

func TestDistance(t *testing.T) {
	tests := []struct {
		name  string
		board func(t *testing.T) Board
		seat  int
		want  int
	}{
		{name: "open board", board: func(t *testing.T) Board { return NewBoard() }, seat: 0, want: 8},
		{name: "black open board", board: func(t *testing.T) Board { return NewBoard() }, seat: 1, want: 8},
		{name: "on the goal row", board: func(t *testing.T) Board { return board(t, "c9", "e1") }, seat: 0, want: 0},
		{name: "detour around a wall", board: func(t *testing.T) Board { return board(t, "e1", "e9", "d1h") }, seat: 0, want: 9},
		{name: "long detour", board: func(t *testing.T) Board { return board(t, "e1", "e9", "a1h", "c1h", "e1h", "g1h") }, seat: 0, want: 12},
		{name: "sealed", board: func(t *testing.T) Board { return board(t, "e1", "e9", "a1h", "c1h", "e1h", "g1h", "h1v") }, seat: 0, want: Unreachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.board(t)
			if got := Distance(b, b.Pawns[tt.seat], GoalRow(tt.seat)); got != tt.want {
				t.Errorf("Distance() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLegalMovesOpening(t *testing.T) {
	if got := len(NewBoard().LegalMoves(0)); got != 3+2*8*8 {
		t.Errorf("opening moves = %d, want %d", got, 3+2*8*8)
	}
}

func stateWith(t *testing.T, d data, turn int) engine.State {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := engine.NewState(Key, []engine.PlayerID{"white", "black"}, raw)
	s.Turn = turn
	return s
}

func TestApplyMove(t *testing.T) {
	g := New()
	tests := []struct {
		name    string
		state   func(t *testing.T) engine.State
		player  engine.PlayerID
		move    string
		wantErr error
		check   func(t *testing.T, s engine.State)
	}{
		{
			name:   "pawn step",
			state:  func(t *testing.T) engine.State { return g.Init([]engine.PlayerID{"white", "black"}) },
			player: "white", move: "e2",
			check: func(t *testing.T, s engine.State) {
				snap, _ := View(s)
				if snap.Pawns[0] != "e2" || s.Turn != 1 || snap.LastMove != "e2" {
					t.Errorf("snapshot = %+v, turn %d", snap, s.Turn)
				}
			},
		},
		{
			name:   "wall spends one of ten",
			state:  func(t *testing.T) engine.State { return g.Init([]engine.PlayerID{"white", "black"}) },
			player: "white", move: "e8h",
			check: func(t *testing.T, s engine.State) {
				snap, _ := View(s)
				if !reflect.DeepEqual(snap.Left, []uint32{9, 10}) || !reflect.DeepEqual(snap.Walls, []string{"e8h"}) {
					t.Errorf("snapshot = %+v", snap)
				}
				if len(snap.Legal) == 0 || snap.Legal[0] != "d9" {
					t.Errorf("black moves = %v", snap.Legal)
				}
			},
		},
		{
			name: "reaching the far row wins",
			state: func(t *testing.T) engine.State {
				return stateWith(t, data{Pawns: [2]string{"e8", "a9"}, Walls: []string{}, Left: [2]int{3, 4}, Moves: []string{}}, 0)
			},
			player: "white", move: "e9",
			check: func(t *testing.T, s engine.State) {
				over, winner := s.Outcome()
				if !over || winner == nil || *winner != "white" {
					t.Errorf("outcome = %v %v", over, winner)
				}
				if snap, _ := View(s); len(snap.Legal) != 0 {
					t.Errorf("legal moves after the end = %v", snap.Legal)
				}
			},
		},
		{
			name: "black wins on row one",
			state: func(t *testing.T) engine.State {
				return stateWith(t, data{Pawns: [2]string{"a5", "c2"}, Walls: []string{}, Left: [2]int{3, 4}, Moves: []string{}}, 1)
			},
			player: "black", move: "c1",
			check: func(t *testing.T, s engine.State) {
				if _, winner := s.Outcome(); winner == nil || *winner != "black" {
					t.Errorf("winner = %v", winner)
				}
			},
		},
		{
			name:   "two squares at once",
			state:  func(t *testing.T) engine.State { return g.Init([]engine.PlayerID{"white", "black"}) },
			player: "white", move: "e3", wantErr: ErrIllegalMove,
		},
		{
			name: "wall that seals a path",
			state: func(t *testing.T) engine.State {
				return stateWith(t, data{Pawns: [2]string{"e1", "e9"}, Walls: []string{"a1h", "c1h", "e1h", "g1h"}, Left: [2]int{6, 10}, Moves: []string{}}, 0)
			},
			player: "white", move: "h1v", wantErr: ErrIllegalMove,
		},
		{
			name:   "out of turn",
			state:  func(t *testing.T) engine.State { return g.Init([]engine.PlayerID{"white", "black"}) },
			player: "black", move: "e8", wantErr: engine.ErrOutOfTurn,
		},
		{
			name:   "garbage",
			state:  func(t *testing.T) engine.State { return g.Init([]engine.PlayerID{"white", "black"}) },
			player: "white", move: "z9", wantErr: ErrBadMove,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.state(t)
			next, err := g.ApplyMove(s, NewMove(tt.player, s.Ply, tt.move))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ApplyMove() error = %v, want %v", err, tt.wantErr)
			}
			if tt.check != nil {
				tt.check(t, next)
			}
		})
	}
}

func TestValidMovesFollowLegality(t *testing.T) {
	g := New()
	s := g.Init([]engine.PlayerID{"white", "black"})
	for _, m := range g.ValidMoves(s, "white") {
		if _, err := g.ApplyMove(s, m); err != nil {
			t.Fatalf("valid move rejected: %s: %v", m.Data, err)
		}
	}
	if g.ValidMoves(s, "black") != nil {
		t.Error("black has moves on white's turn")
	}
}

func naiveLegal(b Board, seat int) []string {
	out := names(b.PawnMoves(seat))
	for r := 0; r < Size-1; r++ {
		for c := 0; c < Size-1; c++ {
			for _, d := range []Orientation{Horizontal, Vertical} {
				w := Wall{At(r, c), d}
				if b.Left[seat] == 0 || b.overlaps(w) {
					continue
				}
				next := b.with(w)
				if HasPath(next, next.Pawns[0], GoalRow(0)) && HasPath(next, next.Pawns[1], GoalRow(1)) {
					out = append(out, w.String())
				}
			}
		}
	}
	return out
}

func moveNames(moves []Move) []string {
	out := []string{}
	for _, m := range moves {
		out = append(out, m.String())
	}
	return out
}

func TestLegalMovesMatchExhaustiveSearchOnRandomGames(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	positions := 0
	for range 300 {
		b := NewBoard()
		seat := 0
		for ply := 0; ply < 120; ply++ {
			got := moveNames(b.LegalMoves(seat))
			want := naiveLegal(b, seat)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("ply %d seat %d walls %v: got %v, want %v", ply, seat, b.Walls(), got, want)
			}
			positions++
			legal := b.LegalMoves(seat)
			var m Move
			if rng.IntN(3) == 0 || b.Left[seat] == 0 {
				pawns := b.PawnMoves(seat)
				m = Move{To: pawns[rng.IntN(len(pawns))]}
			} else {
				m = legal[rng.IntN(len(legal))]
			}
			b = b.Play(seat, m)
			if b.Pawns[seat].Row() == GoalRow(seat) {
				break
			}
			seat = 1 - seat
		}
	}
	if positions < 10_000 {
		t.Fatal("too few positions", positions)
	}
}

func BenchmarkLegalMoves(b *testing.B) {
	board := NewBoard()
	board.Pawns = [2]Cell{At(3, 4), At(5, 3)}
	for _, raw := range []string{"c3h", "e3h", "d5v", "f6h", "b7v", "g2v"} {
		m, _ := ParseMove(raw)
		board = board.with(*m.Wall)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = board.LegalMoves(0)
	}
}

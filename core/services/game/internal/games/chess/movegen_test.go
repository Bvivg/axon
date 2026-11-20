package chess

import "testing"

func perft(p Position, depth int) int {
	if depth == 0 {
		return 1
	}
	moves := p.LegalMoves()
	if depth == 1 {
		return len(moves)
	}
	n := 0
	for _, m := range moves {
		n += perft(p.Play(m), depth-1)
	}
	return n
}

func TestPerft(t *testing.T) {
	tests := []struct {
		name  string
		fen   string
		nodes []int
	}{
		{"start", StartFEN, []int{20, 400, 8902, 197281}},
		{"kiwipete", "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1", []int{48, 2039, 97862}},
		{"position 3", "8/2p5/3p4/KP5r/1R3p1k/8/4P1P1/8 w - - 0 1", []int{14, 191, 2812, 43238}},
		{"position 4", "r3k2r/Pppp1ppp/1b3nbN/nP6/BBP1P3/q4N2/Pp1P2PP/R2Q1RK1 w kq - 0 1", []int{6, 264, 9467}},
		{"position 5", "rnbq1k1r/pp1Pbppp/2p5/8/2B5/8/PPP1NnPP/RNBQK2R w KQ - 1 8", []int{44, 1486, 62379}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, err := ParseFEN(tt.fen)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tt.nodes {
				if testing.Short() && want > 20000 {
					break
				}
				if got := perft(pos, i+1); got != want {
					t.Fatalf("depth %d: got %d, want %d", i+1, got, want)
				}
			}
		})
	}
}

func TestFENRoundTrip(t *testing.T) {
	for _, fen := range []string{
		StartFEN,
		"r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1",
		"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3",
		"8/8/8/8/8/8/8/k6K b - - 42 99",
	} {
		pos, err := ParseFEN(fen)
		if err != nil {
			t.Fatal(fen, err)
		}
		if got := pos.FEN(); got != fen {
			t.Errorf("FEN() = %q, want %q", got, fen)
		}
	}
	for _, bad := range []string{"", "8/8/8/8/8/8/8/8 w - - 0 1", "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR x KQkq - 0 1", "rnbqkbnr/ppppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"} {
		if _, err := ParseFEN(bad); err == nil {
			t.Errorf("ParseFEN(%q) accepted", bad)
		}
	}
}

func TestSAN(t *testing.T) {
	tests := []struct {
		fen, uci, want string
	}{
		{StartFEN, "g1f3", "Nf3"},
		{StartFEN, "e2e4", "e4"},
		{"rnbqkbnr/ppp1p1pp/8/3pPp2/8/8/PPPP1PPP/RNBQKBNR w KQkq f6 0 3", "e5f6", "exf6"},
		{"r3k2r/8/8/8/8/8/8/R3K2R w KQkq - 0 1", "e1g1", "O-O"},
		{"r3k2r/8/8/8/8/8/8/R3K2R b KQkq - 0 1", "e8c8", "O-O-O"},
		{"7k/4P3/8/8/8/8/8/K7 w - - 0 1", "e7e8q", "e8=Q+"},
		{"7k/4P3/8/8/8/8/8/K7 w - - 0 1", "e7e8n", "e8=N"},
		{"k7/8/8/8/8/8/8/KN3N2 w - - 0 1", "b1d2", "Nbd2"},
		{"k7/8/8/1N6/8/8/8/KN6 w - - 0 1", "b1c3", "N1c3"},
		{"rnbqkbnr/pppp1ppp/8/4p3/2B1P3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1", "f3f7", "Qxf7#"},
	}
	for _, tt := range tests {
		pos, err := ParseFEN(tt.fen)
		if err != nil {
			t.Fatal(err)
		}
		m, err := ParseUCI(tt.uci)
		if err != nil {
			t.Fatal(err)
		}
		if got := pos.SAN(m); got != tt.want {
			t.Errorf("SAN(%s) = %q, want %q", tt.uci, got, tt.want)
		}
	}
}

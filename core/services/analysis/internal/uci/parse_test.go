package uci

import (
	"bufio"
	"os"
	"slices"
	"testing"
)

func recorded(t *testing.T) [][]string {
	t.Helper()
	file, err := os.Open("testdata/stockfish-17.1.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var searches [][]string
	var current []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if _, ok := ParseInfo(line); !ok {
			if _, best := ParseBestMove(line); !best {
				continue
			}
		}
		current = append(current, line)
		if _, best := ParseBestMove(line); best {
			searches = append(searches, current)
			current = nil
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return searches
}

func TestParseRecordedSearches(t *testing.T) {
	want := []Result{
		{Depth: 12, Score: Score{Value: 38}, BestMove: "g7g6"},
		{Depth: 12, Score: Score{Mate: true, Value: 1}, BestMove: "h5f7"},
		{Depth: 0, Score: Score{Mate: true, Value: 0}},
		{Depth: 12, Score: Score{Mate: true, Value: -2}, BestMove: "h8g8"},
		{Depth: 0, Score: Score{}},
		{Depth: 12, Score: Score{Value: 38}, BestMove: "b5a4"},
	}
	searches := recorded(t)
	if len(searches) != len(want) {
		t.Fatalf("recording holds %d searches, want %d", len(searches), len(want))
	}
	for i, lines := range searches {
		var got Result
		for _, line := range lines {
			if info, ok := ParseInfo(line); ok && info.HasScore {
				got.Depth, got.Score = info.Depth, info.Score
			}
			if best, ok := ParseBestMove(line); ok {
				got.BestMove = best
			}
		}
		if got != want[i] {
			t.Errorf("search %d = %+v, want %+v", i, got, want[i])
		}
	}
}

func TestParseInfo(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Info
		ok   bool
	}{
		{name: "centipawns with pv", line: "info depth 12 seldepth 23 multipv 1 score cp 38 nodes 42477 nps 196652 hashfull 16 tbhits 0 time 216 pv g7g6 h5f3 g8f6",
			want: Info{Depth: 12, MultiPV: 1, Score: Score{Value: 38}, HasScore: true, PV: []string{"g7g6", "h5f3", "g8f6"}}, ok: true},
		{name: "negative centipawns", line: "info depth 1 seldepth 2 multipv 1 score cp -552 nodes 1 pv h8g8",
			want: Info{Depth: 1, MultiPV: 1, Score: Score{Value: -552}, HasScore: true, PV: []string{"h8g8"}}, ok: true},
		{name: "mate for the side to move", line: "info depth 5 seldepth 2 multipv 1 score mate 1 nodes 217 pv h5f7",
			want: Info{Depth: 5, MultiPV: 1, Score: Score{Mate: true, Value: 1}, HasScore: true, PV: []string{"h5f7"}}, ok: true},
		{name: "checkmated", line: "info depth 0 score mate 0", want: Info{Score: Score{Mate: true}, HasScore: true}, ok: true},
		{name: "stalemate", line: "info depth 0 score cp 0", want: Info{HasScore: true}, ok: true},
		{name: "lower bound", line: "info depth 9 seldepth 12 multipv 1 score cp 51 lowerbound nodes 900 pv e2e4",
			want: Info{Depth: 9, MultiPV: 1, Score: Score{Value: 51}, HasScore: true, Bound: true, PV: []string{"e2e4"}}, ok: true},
		{name: "upper bound mate", line: "info depth 9 score mate -3 upperbound",
			want: Info{Depth: 9, Score: Score{Mate: true, Value: -3}, HasScore: true, Bound: true}, ok: true},
		{name: "current move without a score", line: "info depth 12 currmove e2e4 currmovenumber 1", want: Info{Depth: 12}, ok: true},
		{name: "info string", line: "info string NNUE evaluation using nn-1c0000000000.nnue (133MiB)", ok: false},
		{name: "bare info", line: "info", ok: false},
		{name: "broken score", line: "info depth 3 score cp x", ok: false},
		{name: "unknown score unit", line: "info depth 3 score wdl 1 2 3", ok: false},
		{name: "truncated score", line: "info depth 3 score cp", ok: false},
		{name: "bestmove", line: "bestmove e2e4", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseInfo(tt.line)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.Depth != tt.want.Depth || got.MultiPV != tt.want.MultiPV || got.Score != tt.want.Score || got.HasScore != tt.want.HasScore ||
				got.Bound != tt.want.Bound || !slices.Equal(got.PV, tt.want.PV) {
				t.Fatalf("ParseInfo() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseBestMove(t *testing.T) {
	tests := []struct {
		line string
		want string
		ok   bool
	}{
		{"bestmove g7g6 ponder h5f3", "g7g6", true},
		{"bestmove h5f7", "h5f7", true},
		{"bestmove e7e8q", "e7e8q", true},
		{"bestmove (none)", "", true},
		{"bestmove 0000", "", true},
		{"bestmove", "", false},
		{"info depth 1 pv e2e4", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseBestMove(tt.line)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseBestMove(%q) = %q, %v, want %q, %v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseName(t *testing.T) {
	if name, ok := ParseName("id name Stockfish 17.1"); !ok || name != "Stockfish 17.1" {
		t.Fatalf("ParseName() = %q, %v", name, ok)
	}
	if _, ok := ParseName("id author the Stockfish developers"); ok {
		t.Fatal("the author line is not a name")
	}
}

func TestValidMove(t *testing.T) {
	for move, want := range map[string]bool{
		"e2e4": true, "e7e8q": true, "a1h8": true, "e7e8k": false, "e2e9": false, "e2": false,
		"e2e4\ngo infinite": false, "": false, "E2E4": false,
	} {
		if ValidMove(move) != want {
			t.Errorf("ValidMove(%q) = %v", move, !want)
		}
	}
}

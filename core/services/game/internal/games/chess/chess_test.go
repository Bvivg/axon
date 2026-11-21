package chess

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

const (
	white engine.PlayerID = "white"
	black engine.PlayerID = "black"
)

func stateAt(t *testing.T, fen string) engine.State {
	t.Helper()
	pos, err := ParseFEN(fen)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(data{FEN: fen, Moves: []string{}, SAN: []string{}, Positions: []string{pos.RepetitionKey()}})
	s := engine.NewState(Key, []engine.PlayerID{white, black}, raw)
	s.Turn = int(pos.Turn)
	return s
}

func play(t *testing.T, s engine.State, moves ...string) (engine.State, error) {
	t.Helper()
	g := New()
	for _, uci := range moves {
		player := s.Players[s.Turn]
		next, err := g.ApplyMove(s, NewMove(player, s.Ply, uci))
		if err != nil {
			return s, err
		}
		s = next
	}
	return s, nil
}

func endOf(t *testing.T, s engine.State) End {
	t.Helper()
	snap, err := View(s)
	if err != nil {
		t.Fatal(err)
	}
	return snap.End
}

func TestApplyMoveOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		fen    string
		moves  []string
		status engine.Status
		winner int
		end    End
	}{
		{"fool's mate", StartFEN, []string{"f2f3", "e7e5", "g2g4", "d8h4"}, engine.StatusWin, 1, Checkmate},
		{"scholar's mate", StartFEN, []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}, engine.StatusWin, 0, Checkmate},
		{"stalemate", "7k/5Q2/6K1/8/8/8/8/8 w - - 0 1", []string{"f7f6"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
		{"stalemate by queen", "k7/8/1K6/8/8/8/8/2Q5 w - - 0 1", []string{"c1c7"}, engine.StatusDraw, engine.NoSeat, Stalemate},
		{"king takes the last piece", "8/8/8/8/8/k7/2q5/2K5 w - - 0 1", []string{"c1c2"}, engine.StatusDraw, engine.NoSeat, InsufficientMaterial},
		{"king and knight left", "k6n/8/8/8/8/8/8/2rK4 w - - 0 1", []string{"d1c1"}, engine.StatusDraw, engine.NoSeat, InsufficientMaterial},
		{"same-colour bishops", "k1b5/8/8/8/8/8/8/2rKB3 w - - 0 1", []string{"d1c1"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
		{"two knights can still mate", "k5nn/8/8/8/8/8/8/2rK4 w - - 0 1", []string{"d1c1"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
		{"fifty-move rule", "7k/8/8/8/8/8/8/R6K w - - 99 80", []string{"a1a2"}, engine.StatusDraw, engine.NoSeat, FiftyMoves},
		{"fifty moves reset by pawn move", "7k/8/8/8/8/8/P7/R6K w - - 99 80", []string{"a2a3"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
		{"threefold repetition", StartFEN, []string{"g1f3", "g8f6", "f3g1", "f6g8", "g1f3", "g8f6", "f3g1", "f6g8"}, engine.StatusDraw, engine.NoSeat, ThreefoldRepetition},
		{"en passant", "rnbqkbnr/pppppppp/8/4P3/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 2", []string{"d7d5", "e5d6"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
		{"promotion to knight with check", "8/5P1k/8/8/8/8/p7/K7 w - - 0 1", []string{"f7f8n"}, engine.StatusInProgress, engine.NoSeat, Ongoing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := play(t, stateAt(t, tt.fen), tt.moves...)
			if err != nil {
				t.Fatal(err)
			}
			if s.Status != tt.status || s.Winner != tt.winner {
				t.Fatalf("status %s winner %d, want %s %d", s.Status, s.Winner, tt.status, tt.winner)
			}
			if got := endOf(t, s); got != tt.end {
				t.Fatalf("end %q, want %q", got, tt.end)
			}
		})
	}
}

func TestEnPassantAndPromotionBoard(t *testing.T) {
	s, err := play(t, stateAt(t, "rnbqkbnr/pppppppp/8/4P3/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 2"), "d7d5", "e5d6")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := View(s)
	if snap.FEN != "rnbqkbnr/ppp1pppp/3P4/8/8/8/PPPP1PPP/RNBQKBNR b KQkq - 0 3" || snap.SAN[1] != "exd6" || snap.LastMove != "e5d6" {
		t.Fatal(snap)
	}
	s, err = play(t, stateAt(t, "8/5P1k/8/8/8/8/p7/K7 w - - 0 1"), "f7f8n")
	if err != nil {
		t.Fatal(err)
	}
	snap, _ = View(s)
	if snap.FEN != "5N2/7k/8/8/8/8/p7/K7 b - - 0 1" || snap.SAN[0] != "f8=N+" || !snap.Check {
		t.Fatal(snap)
	}
}

func TestApplyMoveRejects(t *testing.T) {
	tests := []struct {
		name string
		fen  string
		uci  string
		err  error
	}{
		{"pinned knight", "4k3/4r3/8/8/8/8/4N3/4K3 w - - 0 1", "e2c3", ErrIllegalMove},
		{"king into check", "4k3/8/8/8/8/8/3r4/4K3 w - - 0 1", "e1d1", nil},
		{"king next to king", "8/8/8/3k4/8/3K4/8/8 w - - 0 1", "d3d4", ErrIllegalMove},
		{"castle through attack", "4k3/8/8/8/8/8/5r2/4K2R w K - 0 1", "e1g1", ErrIllegalMove},
		{"castle out of check", "4k3/4r3/8/8/8/8/8/4K2R w K - 0 1", "e1g1", ErrIllegalMove},
		{"castle without rights", "4k3/8/8/8/8/8/8/4K2R w - - 0 1", "e1g1", ErrIllegalMove},
		{"queenside rook path blocked", "4k3/8/8/8/8/8/8/RN2K3 w Q - 0 1", "e1c1", ErrIllegalMove},
		{"promotion without piece", "7k/4P3/8/8/8/8/8/K7 w - - 0 1", "e7e8", ErrIllegalMove},
		{"pawn backwards", StartFEN, "e2e1", ErrIllegalMove},
		{"garbage", StartFEN, "zz99", ErrBadMove},
		{"bad promotion letter", "7k/4P3/8/8/8/8/8/K7 w - - 0 1", "e7e8k", ErrBadMove},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := play(t, stateAt(t, tt.fen), tt.uci)
			if tt.err == nil {
				if err == nil {
					return
				}
				if errors.Is(err, ErrIllegalMove) {
					return
				}
				t.Fatal(err)
			}
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestEngineContract(t *testing.T) {
	g := New()
	s := g.Init([]engine.PlayerID{white, black})
	if len(g.ValidMoves(s, white)) != 20 || g.ValidMoves(s, black) != nil {
		t.Fatal("valid moves at start")
	}
	if _, err := g.ApplyMove(s, NewMove(black, 0, "e7e5")); !errors.Is(err, engine.ErrOutOfTurn) {
		t.Fatal(err)
	}
	if _, err := g.ApplyMove(s, NewMove(white, 3, "e2e4")); !errors.Is(err, engine.ErrStaleMove) {
		t.Fatal(err)
	}
	s, err := play(t, s, "f2f3", "e7e5", "g2g4", "d8h4")
	if err != nil {
		t.Fatal(err)
	}
	over, winner := g.IsTerminal(s)
	if !over || winner == nil || *winner != black || g.ValidMoves(s, white) != nil {
		t.Fatal("terminal", over, winner)
	}
	if _, err := g.ApplyMove(s, NewMove(white, s.Ply, "e2e4")); !errors.Is(err, engine.ErrGameOver) {
		t.Fatal(err)
	}
}

func TestMovesReadsThePlayedUCIMoves(t *testing.T) {
	s, err := play(t, New().Init([]engine.PlayerID{white, black}), "e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7")
	if err != nil {
		t.Fatal(err)
	}
	moves, err := Moves(s)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}; !slices.Equal(moves, want) || !s.Over() {
		t.Fatalf("Moves() = %v (over %v), want %v", moves, s.Over(), want)
	}
	if moves, err := Moves(New().Init([]engine.PlayerID{white, black})); err != nil || len(moves) != 0 {
		t.Fatalf("Moves() of the start = %v, %v", moves, err)
	}
	if _, err := Moves(engine.State{Game: Key}); !errors.Is(err, engine.ErrStateMalformed) {
		t.Fatalf("Moves() of an empty state = %v", err)
	}
}

func TestTheMoveLimitDrawsALongGame(t *testing.T) {
	if def := Definition(); def.MaxPlies != MaxPlies || MaxPlies < 538 {
		t.Fatalf("MaxPlies = %d (definition %d), want at least the longest recorded game", MaxPlies, def.MaxPlies)
	}
	s := New().Init([]engine.PlayerID{white, black})
	s.MaxPlies = 4
	drawn, err := play(t, s, "e2e4", "e7e5", "g1f3", "b8c6")
	if err != nil {
		t.Fatal(err)
	}
	over, winner := New().IsTerminal(drawn)
	if !over || winner != nil || drawn.Status != engine.StatusDraw || !drawn.MoveLimit || endOf(t, drawn) != Ongoing {
		t.Fatalf("after the limit = over %v winner %v status %q limit %v end %q", over, winner, drawn.Status, drawn.MoveLimit, endOf(t, drawn))
	}
	if New().ValidMoves(drawn, white) != nil {
		t.Fatal("moves are still offered after the limit")
	}
	mated, err := play(t, s, "f2f3", "e7e5", "g2g4", "d8h4")
	if err != nil {
		t.Fatal(err)
	}
	if _, winner := mated.Outcome(); winner == nil || *winner != black || mated.MoveLimit || endOf(t, mated) != Checkmate {
		t.Fatalf("mate on the last ply = winner %v limit %v end %q", winner, mated.MoveLimit, endOf(t, mated))
	}
}

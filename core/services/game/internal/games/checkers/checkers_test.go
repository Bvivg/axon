package checkers

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

var players = []engine.PlayerID{"black", "white"}

func stateOf(t *testing.T, b Board, turn, ply, quiet int, moves ...string) engine.State {
	t.Helper()
	if moves == nil {
		moves = []string{}
	}
	raw, err := json.Marshal(data{Board: b.String(), Moves: moves, Quiet: quiet})
	if err != nil {
		t.Fatal(err)
	}
	state := engine.NewState(Key, players, raw)
	state.Turn, state.Ply = turn, ply
	return state
}

func snapshot(t *testing.T, state engine.State) Snapshot {
	t.Helper()
	snap, err := View(state)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func TestApplyMove(t *testing.T) {
	tests := []struct {
		name      string
		state     func(t *testing.T) engine.State
		player    engine.PlayerID
		move      string
		err       error
		status    engine.Status
		winner    int
		end       End
		quiet     int
		board     string
		lastMove  string
		nextLegal []string
	}{
		{
			name:      "black opens",
			state:     func(t *testing.T) engine.State { return Game{}.Init(players) },
			player:    "black",
			move:      "11-15",
			status:    engine.StatusInProgress,
			winner:    engine.NoSeat,
			board:     "bbbbbbbbbb.b..b.....wwwwwwwwwwww",
			lastMove:  "11-15",
			nextLegal: []string{"21-17", "22-17", "22-18", "23-18", "23-19", "24-19", "24-20"},
		},
		{name: "white may not open", state: func(t *testing.T) engine.State { return Game{}.Init(players) }, player: "white", move: "22-18", err: engine.ErrOutOfTurn},
		{name: "a man cannot step backwards", state: func(t *testing.T) engine.State { return Game{}.Init(players) }, player: "black", move: "9-5", err: ErrIllegalMove},
		{name: "garbage notation", state: func(t *testing.T) engine.State { return Game{}.Init(players) }, player: "black", move: "e2e4", err: ErrBadMove},
		{
			name: "a step is refused while a capture exists",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{15: BlackMan, 1: BlackMan, 18: WhiteMan, 32: WhiteMan}), 0, 4, 0)
			},
			player: "black", move: "1-6", err: ErrIllegalMove,
		},
		{
			name: "a partial multi-jump is refused",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{6: BlackMan, 10: WhiteMan, 19: WhiteMan, 32: WhiteMan}), 0, 4, 0)
			},
			player: "black", move: "6x15", err: ErrIllegalMove,
		},
		{
			name: "the shorter branch of a multi-jump is allowed",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{6: BlackMan, 10: WhiteMan, 18: WhiteMan, 19: WhiteMan, 27: WhiteMan}), 0, 10, 5)
			},
			player: "black", move: "6x15x22", status: engine.StatusInProgress, winner: engine.NoSeat,
			board:     position(map[int]Piece{22: BlackMan, 19: WhiteMan, 27: WhiteMan}).String(),
			lastMove:  "6x15x22",
			nextLegal: []string{"19-15", "19-16", "27-23", "27-24"},
		},
		{
			name: "crowning ends the jump and the turn",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{22: BlackMan, 26: WhiteMan, 27: WhiteMan}), 0, 20, 0)
			},
			player: "black", move: "22x31", status: engine.StatusInProgress, winner: engine.NoSeat,
			board:     position(map[int]Piece{31: BlackKing, 27: WhiteMan}).String(),
			lastMove:  "22x31",
			nextLegal: []string{"27-23", "27-24"},
		},
		{
			name: "taking the last piece wins",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{15: BlackMan, 18: WhiteMan}), 0, 30, 0)
			},
			player: "black", move: "15x22", status: engine.StatusWin, winner: 0, end: NoMoves,
			board: position(map[int]Piece{22: BlackMan}).String(), lastMove: "15x22",
		},
		{
			name: "blocking the last piece wins",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{1: BlackMan, 25: BlackMan, 22: BlackMan, 29: WhiteMan}), 0, 30, 0)
			},
			player: "black", move: "1-5", status: engine.StatusWin, winner: 0, end: NoMoves,
			board: position(map[int]Piece{5: BlackMan, 25: BlackMan, 22: BlackMan, 29: WhiteMan}).String(), lastMove: "1-5",
		},
		{
			name: "white wins on its move too",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: WhiteMan, 15: BlackMan}), 1, 31, 0)
			},
			player: "white", move: "18x11", status: engine.StatusWin, winner: 1, end: NoMoves,
			board: position(map[int]Piece{11: WhiteMan}).String(), lastMove: "18x11",
		},
		{
			name: "a quiet king move counts towards the draw",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: BlackKing, 1: WhiteKing}), 0, 40, 10)
			},
			player: "black", move: "18-22", status: engine.StatusInProgress, winner: engine.NoSeat, quiet: 11,
			board: position(map[int]Piece{22: BlackKing, 1: WhiteKing}).String(), lastMove: "18-22", nextLegal: []string{"1-5", "1-6"},
		},
		{
			name: "forty moves each without a capture or a man move draw",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: BlackKing, 1: WhiteKing}), 0, 120, QuietLimit-1)
			},
			player: "black", move: "18-22", status: engine.StatusDraw, winner: engine.NoSeat, end: FortyMoves, quiet: QuietLimit,
			board: position(map[int]Piece{22: BlackKing, 1: WhiteKing}).String(), lastMove: "18-22",
		},
		{
			name: "a man move resets the count",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: BlackKing, 9: BlackMan, 32: WhiteKing}), 0, 120, QuietLimit-1)
			},
			player: "black", move: "9-13", status: engine.StatusInProgress, winner: engine.NoSeat,
			board: position(map[int]Piece{18: BlackKing, 13: BlackMan, 32: WhiteKing}).String(), lastMove: "9-13", nextLegal: []string{"32-27", "32-28"},
		},
		{
			name: "a king capture resets the count",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: BlackKing, 22: WhiteMan, 1: WhiteKing}), 0, 120, QuietLimit-1)
			},
			player: "black", move: "18x25", status: engine.StatusInProgress, winner: engine.NoSeat,
			board: position(map[int]Piece{25: BlackKing, 1: WhiteKing}).String(), lastMove: "18x25", nextLegal: []string{"1-5", "1-6"},
		},
		{
			name: "the ply cap ends the game in a draw",
			state: func(t *testing.T) engine.State {
				return stateOf(t, position(map[int]Piece{18: BlackKing, 9: BlackMan, 32: WhiteKing}), 0, MaxPlies-1, 0)
			},
			player: "black", move: "9-13", status: engine.StatusDraw, winner: engine.NoSeat, end: MoveLimit,
			board: position(map[int]Piece{18: BlackKing, 13: BlackMan, 32: WhiteKing}).String(), lastMove: "9-13",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.state(t)
			next, err := Game{}.ApplyMove(before, NewMove(tt.player, before.Ply, tt.move))
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("ApplyMove() error = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ApplyMove() error = %v", err)
			}
			if next.Status != tt.status || next.Winner != tt.winner || next.Ply != before.Ply+1 {
				t.Fatalf("status %s, winner %d, ply %d", next.Status, next.Winner, next.Ply)
			}
			snap := snapshot(t, next)
			if snap.Board != tt.board || snap.End != tt.end || snap.Quiet != tt.quiet || snap.LastMove != tt.lastMove {
				t.Fatalf("snapshot = %+v", snap)
			}
			if !slices.Equal(sorted(snap.Legal), sorted(tt.nextLegal)) {
				t.Fatalf("next legal = %v, want %v", snap.Legal, tt.nextLegal)
			}
			if over, winner := (Game{}).IsTerminal(next); over != next.Over() || over && tt.winner >= 0 && (winner == nil || *winner != players[tt.winner]) {
				t.Fatalf("IsTerminal() = %v, %v", over, winner)
			}
		})
	}
}

func TestDecodeRejectsMalformedState(t *testing.T) {
	for name, payload := range map[string]string{
		"not json":       `{`,
		"short board":    `{"board":"bbb","moves":[],"quiet":0}`,
		"unknown piece":  `{"board":"bbbbbbbbbbbb........wwwwwwwwwwwq","moves":[],"quiet":0}`,
		"negative quiet": `{"board":"bbbbbbbbbbbb........wwwwwwwwwwww","moves":[],"quiet":-1}`,
		"quiet too long": `{"board":"bbbbbbbbbbbb........wwwwwwwwwwww","moves":[],"quiet":81}`,
	} {
		t.Run(name, func(t *testing.T) {
			state := engine.NewState(Key, players, json.RawMessage(payload))
			if _, err := (Game{}).ApplyMove(state, NewMove("black", 0, "11-15")); !errors.Is(err, engine.ErrStateMalformed) {
				t.Fatalf("ApplyMove() error = %v, want ErrStateMalformed", err)
			}
			if moves := (Game{}).ValidMoves(state, "black"); moves != nil {
				t.Fatalf("ValidMoves() = %v", moves)
			}
		})
	}
}

func TestMalformedMove(t *testing.T) {
	state := Game{}.Init(players)
	_, err := Game{}.ApplyMove(state, engine.Move{Player: "black", Ply: 0, Data: json.RawMessage(`[1]`)})
	if !errors.Is(err, engine.ErrMoveMalformed) {
		t.Fatalf("ApplyMove() error = %v, want ErrMoveMalformed", err)
	}
}

func TestValidMoves(t *testing.T) {
	state := Game{}.Init(players)
	if moves := (Game{}).ValidMoves(state, "white"); moves != nil {
		t.Fatalf("white got moves out of turn: %v", moves)
	}
	if moves := (Game{}).ValidMoves(state, "stranger"); moves != nil {
		t.Fatalf("a stranger got moves: %v", moves)
	}
	moves := Game{}.ValidMoves(state, "black")
	if len(moves) != 7 {
		t.Fatalf("ValidMoves() = %d moves, want 7", len(moves))
	}
	for _, m := range moves {
		if _, err := (Game{}).ApplyMove(state, m); err != nil {
			t.Fatalf("valid move %s refused: %v", m.Data, err)
		}
	}
}

func TestRandomGamesStayConsistent(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 5))
	for game := range 200 {
		state := Game{}.Init(players)
		for !state.Over() {
			player := players[state.Turn]
			moves := Game{}.ValidMoves(state, player)
			if len(moves) == 0 {
				t.Fatalf("game %d: %s has no moves at ply %d but the game goes on", game, player, state.Ply)
			}
			next, err := Game{}.ApplyMove(state, moves[rng.IntN(len(moves))])
			if err != nil {
				t.Fatalf("game %d: valid move refused: %v", game, err)
			}
			snap := snapshot(t, next)
			b, _ := ParseBoard(snap.Board)
			if b.Count(Black) > MenEach || b.Count(White) > MenEach || len(snap.Moves) != next.Ply {
				t.Fatalf("game %d: board %s after %d plies", game, snap.Board, next.Ply)
			}
			state = next
		}
		if state.Ply > MaxPlies {
			t.Fatalf("game %d ran %d plies", game, state.Ply)
		}
	}
}

var shortWin = strings.Fields("9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15 24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9")

func TestShortGameEndsWhenWhiteIsBlocked(t *testing.T) {
	state := Game{}.Init(players)
	for i, move := range shortWin {
		next, err := Game{}.ApplyMove(state, NewMove(players[state.Turn], state.Ply, move))
		if err != nil {
			t.Fatalf("ply %d %s: %v", i+1, move, err)
		}
		state = next
	}
	snap := snapshot(t, state)
	if state.Status != engine.StatusWin || state.Winner != 0 || snap.End != NoMoves || snap.Board != "bbbb.bbbb..w..........B........." {
		t.Fatalf("final state %s winner %d, snapshot %+v", state.Status, state.Winner, snap)
	}
	if !slices.Equal(snap.Moves, shortWin) {
		t.Fatalf("moves = %v", snap.Moves)
	}
}

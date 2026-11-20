package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/checkers"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func TestCheckersBoard(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	players := []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}
	game := checkers.New()
	state, err := game.ApplyMove(game.Init(players), checkers.NewMove(players[0], 0, "11-15"))
	if err != nil {
		t.Fatal(err)
	}
	item := session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: checkers.Key, State: state, UpdatedAt: time.Now()}
	out, err := present(item, session.TimeLimitMS, nil)
	if err != nil {
		t.Fatal(err)
	}
	board := out.GetCheckers()
	squares := board.GetSquares()
	if len(squares) != checkers.Squares || squares[10] != gamev1.CheckersPiece_CHECKERS_PIECE_UNSPECIFIED || squares[14] != gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_MAN || squares[31] != gamev1.CheckersPiece_CHECKERS_PIECE_WHITE_MAN {
		t.Fatal(squares)
	}
	if board.GetLastMove() != "11-15" || len(board.GetMoves()) != 1 || len(board.GetLegalMoves()) != 7 || out.GetTurnUserId() != o.String() {
		t.Fatal(board, out.GetTurnUserId())
	}
	frame, err := historyFrame(item, state)
	if err != nil || frame.GetCheckers().GetLastMove() != "11-15" || len(frame.GetCheckers().GetLegalMoves()) != 0 || frame.GetPly() != 1 {
		t.Fatal(frame, err)
	}
	waiting, err := present(session.Session{ID: uuid.New(), Creator: x, Game: checkers.Key, UpdatedAt: time.Now()}, session.TimeLimitMS, nil)
	if err != nil || len(waiting.GetCheckers().GetSquares()) != checkers.Squares || waiting.GetCheckers().GetSquares()[0] != gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_MAN {
		t.Fatal(waiting, err)
	}
}

func TestCheckersEnd(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	raw := []byte(`{"board":"..............b..w..............","moves":[],"quiet":0}`)
	state := engine.NewState(checkers.Key, []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}, raw)
	next, err := checkers.New().ApplyMove(state, checkers.NewMove(engine.PlayerID(x.String()), 0, "15x22"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := present(session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: checkers.Key, State: next, UpdatedAt: time.Now()}, session.TimeLimitMS, nil)
	if err != nil || out.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || out.GetWinnerUserId() != x.String() || out.GetCheckers().GetEnd() != gamev1.CheckersEnd_CHECKERS_END_NO_MOVES {
		t.Fatal(out, err)
	}
}

func TestCheckersMoveErrors(t *testing.T) {
	for _, err := range []error{checkers.ErrIllegalMove, checkers.ErrBadMove} {
		if got := moveError(fmt.Errorf("session: %w", err)); got != gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE {
			t.Errorf("moveError(%v) = %v", err, got)
		}
	}
}

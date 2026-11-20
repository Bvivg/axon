package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func TestResultClock(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	item := session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: tictactoe.Key,
		State: tictactoe.New().Init([]engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}),
		Clock: session.NewClock(time.Now().Add(-2*time.Second), session.TimeLimitMS), Revision: 7, UpdatedAt: time.Now()}
	out, err := present(item, session.TimeLimitMS, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetClock().GetXRemainingMs() > 298000 || out.GetClock().GetXRemainingMs() < 297000 || out.GetClock().GetORemainingMs() != 300000 {
		t.Fatal(out.Clock)
	}
	if out.GetRevision() != 7 || out.GetTurnUserId() != x.String() || out.Status != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		t.Fatal(out)
	}
	item.State = item.State.Finish(1)
	item.Clock.RemainingMS[0], item.TimedOut = 0, true
	out, err = present(item, session.TimeLimitMS, nil)
	if err != nil || !out.GetTimedOut() || out.GetWinnerUserId() != o.String() || out.GetTurnUserId() != "" || out.GetClock().GetXRemainingMs() != 0 || len(out.TicTacToe.WinningCells) != 0 {
		t.Fatal(out, err)
	}
}

func TestWaitingResultClock(t *testing.T) {
	out, err := present(session.Session{ID: uuid.New(), Creator: uuid.New(), UpdatedAt: time.Now()}, session.TimeLimitMS, nil)
	if err != nil || out.Status != gamev1.SessionStatus_SESSION_STATUS_WAITING || out.Clock.GetXRemainingMs() != 300000 || out.Clock.GetORemainingMs() != 300000 {
		t.Fatal(out, err)
	}
}

func TestQuoridorBoard(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	players := []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}
	game := quoridor.New()
	state, err := game.ApplyMove(game.Init(players), quoridor.NewMove(players[0], 0, "e8h"))
	if err != nil {
		t.Fatal(err)
	}
	item := session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: quoridor.Key, State: state, UpdatedAt: time.Now()}
	out, err := present(item, session.TimeLimitMS, nil)
	if err != nil {
		t.Fatal(err)
	}
	board := out.GetQuoridor()
	if board.GetLastMove() != "e8h" || len(board.GetWalls()) != 1 || board.GetWallsLeft()[0] != 9 || board.GetPawns()[1] != "e9" || out.GetTurnUserId() != o.String() {
		t.Fatal(board)
	}
	if len(board.GetLegalMoves()) == 0 || board.GetLegalMoves()[0] != "d9" {
		t.Fatal(board.GetLegalMoves())
	}
	waiting, err := present(session.Session{ID: uuid.New(), Creator: x, Game: quoridor.Key, UpdatedAt: time.Now()}, session.TimeLimitMS, nil)
	if err != nil || len(waiting.GetQuoridor().GetPawns()) != 2 {
		t.Fatal(waiting, err)
	}
}

func TestTestIdentityNeedsHooks(t *testing.T) {
	user := uuid.New()
	headers := http.Header{}
	headers.Set(TestUserHeader, user.String())
	if _, _, err := New(Config{}).identity(headers); err == nil {
		t.Fatal("test identity accepted without hooks")
	}
	got, claims, err := New(Config{TestHooks: true}).identity(headers)
	if err != nil || got != user || claims.UserID != user || !claims.ExpiresAt.After(time.Now()) {
		t.Fatal(got, claims, err)
	}
	headers.Set(TestUserHeader, "nobody")
	if _, _, err := New(Config{TestHooks: true}).identity(headers); err == nil {
		t.Fatal("malformed test identity accepted")
	}
}

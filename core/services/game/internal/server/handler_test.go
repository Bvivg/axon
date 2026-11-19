package server

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func TestResultClock(t *testing.T) {
	x, o := uuid.New(), uuid.New()
	item := session.Session{ID: uuid.New(), Creator: x, Opponent: &o, Game: tictactoe.Key,
		State: tictactoe.New().Init([]engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}),
		Clock: session.NewClock(time.Now().Add(-2 * time.Second)), Revision: 7, UpdatedAt: time.Now()}
	out, err := result(item, nil)
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
	out, err = result(item, nil)
	if err != nil || !out.GetTimedOut() || out.GetWinnerUserId() != o.String() || out.GetTurnUserId() != "" || out.GetClock().GetXRemainingMs() != 0 || len(out.TicTacToe.WinningCells) != 0 {
		t.Fatal(out, err)
	}
}

func TestWaitingResultClock(t *testing.T) {
	out, err := result(session.Session{ID: uuid.New(), Creator: uuid.New(), UpdatedAt: time.Now()}, nil)
	if err != nil || out.Status != gamev1.SessionStatus_SESSION_STATUS_WAITING || out.Clock.GetXRemainingMs() != 300000 || out.Clock.GetORemainingMs() != 300000 {
		t.Fatal(out, err)
	}
}

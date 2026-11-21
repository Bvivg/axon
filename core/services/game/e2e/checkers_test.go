//go:build e2e

package e2e

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

var checkersWin = strings.Fields("9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15 24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9")

func checkersMove(ply uint32, move string) *gamev1.SessionCommand {
	return &gamev1.SessionCommand{Action: &gamev1.SessionCommand_CheckersMove{CheckersMove: &gamev1.CheckersMove{Ply: ply, Move: move}}}
}

func pieces(squares []gamev1.CheckersPiece, kind gamev1.CheckersPiece) int {
	n := 0
	for _, square := range squares {
		if square == kind {
			n++
		}
	}
	return n
}

func TestCheckersGameToAWinThroughTheGateway(t *testing.T) {
	ctx := t.Context()
	client := games()
	black, white, watcher := register(t), register(t), register(t)
	created, err := client.CreateSession(ctx, as(black, &gamev1.CreateSessionRequest{Game: "checkers", RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal("create:", err)
	}
	id := created.Msg.GetSession().GetId()
	if squares := created.Msg.GetSession().GetCheckers().GetSquares(); len(squares) != 32 || pieces(squares, gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_MAN) != 12 {
		t.Fatalf("waiting board = %v", squares)
	}
	if _, err := client.JoinSession(ctx, as(white, &gamev1.JoinSessionRequest{SessionId: id})); err != nil {
		t.Fatal("join:", err)
	}

	blackSide, whiteSide := open(t, black, id), open(t, white, id)
	start := blackSide.atPly(0)
	whiteSide.atPly(0)
	if start.GetTurnUserId() != black.id || !slices.Equal(start.GetCheckers().GetLegalMoves(), []string{"9-13", "9-14", "10-14", "10-15", "11-15", "11-16", "12-16"}) {
		t.Fatalf("opening = turn %s, legal %v", start.GetTurnUserId(), start.GetCheckers().GetLegalMoves())
	}
	spectator := open(t, watcher, id)
	spectator.atPly(0)

	blackSide.send(checkersMove(0, "9-5"))
	blackSide.until("an illegal move refusal", func(event *gamev1.SessionEvent) bool {
		return event.GetError() == gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE
	})
	blackSide.send(checkersMove(0, strings.Repeat("1x", 30)+"1"))
	blackSide.until("a malformed move refusal", func(event *gamev1.SessionEvent) bool {
		return event.GetError() == gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND
	})
	whiteSide.send(checkersMove(0, "22-18"))
	whiteSide.until("an out of turn refusal", func(event *gamev1.SessionEvent) bool {
		return event.GetError() == gamev1.SessionError_SESSION_ERROR_NOT_YOUR_TURN
	})

	for i, move := range checkersWin {
		ply := uint32(i)
		mover := blackSide
		if i%2 == 1 {
			mover = whiteSide
		}
		if i > 0 {
			mover.atPly(ply)
		}
		time.Sleep(300 * time.Millisecond)
		mover.send(checkersMove(ply, move))
		if i == 0 {
			eventually(t, "the game in the live list", func() bool {
				live, err := client.ListLiveSessions(ctx, as(watcher, &gamev1.ListLiveSessionsRequest{Game: new("checkers")}))
				if err != nil {
					t.Fatal("live list:", err)
				}
				return slices.ContainsFunc(live.Msg.GetSessions(), func(item *gamev1.Session) bool { return item.GetId() == id })
			})
		}
	}
	crowned := spectator.atPly(7).GetCheckers()
	if crowned.GetSquares()[29] != gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_KING || crowned.GetLastMove() != "16x23x30" {
		t.Fatalf("spectator board after the crowning = %v", crowned)
	}
	final := spectator.until("the finished game", func(event *gamev1.SessionEvent) bool {
		return event.GetSession().GetStatus() == gamev1.SessionStatus_SESSION_STATUS_WON
	}).GetSession()
	board := final.GetCheckers()
	if final.GetWinnerUserId() != black.id || final.GetPly() != uint32(len(checkersWin)) || board.GetEnd() != gamev1.CheckersEnd_CHECKERS_END_NO_MOVES || len(board.GetLegalMoves()) != 0 {
		t.Fatalf("final = winner %s, ply %d, board %v", final.GetWinnerUserId(), final.GetPly(), board)
	}
	if pieces(board.GetSquares(), gamev1.CheckersPiece_CHECKERS_PIECE_WHITE_MAN) != 1 || pieces(board.GetSquares(), gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_KING) != 1 || !slices.Equal(board.GetMoves(), checkersWin) {
		t.Fatalf("final board = %v", board)
	}

	var history *gamev1.GetSessionHistoryResponse
	eventually(t, "the projected history", func() bool {
		response, err := client.GetSessionHistory(ctx, as(watcher, &gamev1.GetSessionHistoryRequest{SessionId: id}))
		if err != nil {
			var connectErr *connect.Error
			if errors.As(err, &connectErr) && connectErr.Code() == connect.CodeNotFound {
				return false
			}
			t.Fatal("history:", err)
		}
		history = response.Msg
		return len(history.GetFrames()) == len(checkersWin)+1
	})
	if history.GetSession().GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || history.GetSession().GetWinnerUserId() != black.id {
		t.Fatalf("history session = %v", history.GetSession())
	}
	for ply, frame := range history.GetFrames() {
		checkers := frame.GetCheckers()
		if frame.GetPly() != uint32(ply) || len(checkers.GetSquares()) != 32 || len(checkers.GetLegalMoves()) != 0 {
			t.Fatalf("frame %d = %v", ply, frame)
		}
		if ply == 0 {
			continue
		}
		mover := black.id
		if ply%2 == 0 {
			mover = white.id
		}
		if frame.GetPlayerId() != mover || checkers.GetLastMove() != checkersWin[ply-1] {
			t.Fatalf("frame %d mover %s, last move %q", ply, frame.GetPlayerId(), checkers.GetLastMove())
		}
	}
	if history.GetFrames()[7].GetCheckers().GetSquares()[29] != gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_KING {
		t.Fatal("the replay lost the crowning")
	}
	if history.GetFrames()[len(checkersWin)].GetCheckers().GetEnd() != gamev1.CheckersEnd_CHECKERS_END_NO_MOVES {
		t.Fatal("the last replay frame does not carry the ending")
	}
}

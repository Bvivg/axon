//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

type player struct {
	id    string
	token string
}

type socket struct {
	t    *testing.T
	conn *ws.Conn
}

func gatewayURL() string {
	if url := os.Getenv("GATEWAY_URL"); url != "" {
		return url
	}
	return "http://gateway:8080"
}

func games() gamev1connect.GameServiceClient {
	return gamev1connect.NewGameServiceClient(&http.Client{Timeout: 10 * time.Second}, gatewayURL())
}

func register(t *testing.T) player {
	t.Helper()
	auth := authv1connect.NewAuthServiceClient(&http.Client{Timeout: 10 * time.Second}, gatewayURL())
	registered, err := auth.Register(t.Context(), connect.NewRequest(&authv1.RegisterRequest{
		Email: "watch-" + uuid.NewString()[:8] + "@axon.test", Password: "watch-" + uuid.NewString(),
	}))
	if err != nil {
		t.Fatal("register:", err)
	}
	return player{id: registered.Msg.GetUser().GetId(), token: registered.Msg.GetTokens().GetAccessToken()}
}

func as[T any](who player, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+who.token)
	return req
}

func open(t *testing.T, who player, id string) *socket {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	conn, err := ws.Dial(ctx, strings.Replace(gatewayURL(), "http", "ws", 1)+"/ws/game", ws.DialOptions{
		Subprotocols: []string{"axon.game.v1", "axon.bearer." + who.token},
	})
	if err != nil {
		t.Fatal("dial game socket:", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	s := &socket{t: t, conn: conn}
	s.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: id}})
	return s
}

func (s *socket) send(command *gamev1.SessionCommand) {
	s.t.Helper()
	frame, err := protojson.Marshal(command)
	if err != nil {
		s.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(s.t.Context(), 10*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, frame); err != nil {
		s.t.Fatal("socket write:", err)
	}
}

func (s *socket) until(what string, match func(*gamev1.SessionEvent) bool) *gamev1.SessionEvent {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.t.Context(), 20*time.Second)
	defer cancel()
	for {
		raw, err := s.conn.Read(ctx)
		if err != nil {
			s.t.Fatalf("waiting for %s: %v", what, err)
		}
		var event gamev1.SessionEvent
		if err := protojson.Unmarshal(raw, &event); err != nil {
			s.t.Fatal(err)
		}
		if match(&event) {
			return &event
		}
	}
}

func (s *socket) atPly(ply uint32) *gamev1.Session {
	s.t.Helper()
	return s.until("ply", func(event *gamev1.SessionEvent) bool {
		return event.GetSession() != nil && event.GetSession().GetPly() == ply
	}).GetSession()
}

func move(ply, cell uint32) *gamev1.SessionCommand {
	return &gamev1.SessionCommand{Action: &gamev1.SessionCommand_Move{Move: &gamev1.TicTacToeMove{Ply: ply, Cell: cell}}}
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestSpectatorsWatchLiveAndReplayFinishedGames(t *testing.T) {
	ctx := t.Context()
	client := games()
	x, o, watcher := register(t), register(t), register(t)
	created, err := client.CreateSession(ctx, as(x, &gamev1.CreateSessionRequest{Game: "tictactoe", RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal("create:", err)
	}
	id := created.Msg.GetSession().GetId()
	if _, err := client.GetSessionHistory(ctx, as(watcher, &gamev1.GetSessionHistoryRequest{SessionId: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("history of an open game for a stranger = %v, want NotFound", err)
	}
	if _, err := client.JoinSession(ctx, as(o, &gamev1.JoinSessionRequest{SessionId: id})); err != nil {
		t.Fatal("join:", err)
	}

	xs, ys := open(t, x, id), open(t, o, id)
	xs.atPly(0)
	ys.atPly(0)
	spectator := open(t, watcher, id)
	first := spectator.atPly(0)
	if first.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING || len(first.GetPlayerIds()) != 2 {
		t.Fatalf("spectator snapshot = %v", first)
	}
	xs.until("spectator count", func(event *gamev1.SessionEvent) bool { return event.GetSession().GetSpectators() == 1 })

	spectator.send(move(0, 4))
	spectator.until("spectator refusal", func(event *gamev1.SessionEvent) bool {
		return event.GetError() == gamev1.SessionError_SESSION_ERROR_SPECTATOR
	})

	xs.send(move(0, 0))
	seen := spectator.atPly(1)
	if seen.GetTicTacToe().GetCells()[0] != gamev1.Mark_MARK_X {
		t.Fatalf("spectator board after the first move = %v", seen.GetTicTacToe().GetCells())
	}
	eventually(t, "the game in the live list", func() bool {
		live, err := client.ListLiveSessions(ctx, as(watcher, &gamev1.ListLiveSessionsRequest{Game: new("tictactoe")}))
		if err != nil {
			t.Fatal("live list:", err)
		}
		for _, item := range live.Msg.GetSessions() {
			if item.GetId() == id {
				return true
			}
		}
		return false
	})

	for i, step := range []struct {
		mover *socket
		cell  uint32
	}{{ys, 4}, {xs, 1}, {ys, 5}, {xs, 2}} {
		ply := uint32(i + 1)
		step.mover.atPly(ply)
		step.mover.send(move(ply, step.cell))
	}
	final := spectator.until("the finished game", func(event *gamev1.SessionEvent) bool {
		return event.GetSession().GetStatus() == gamev1.SessionStatus_SESSION_STATUS_WON
	}).GetSession()
	if final.GetWinnerUserId() != x.id || final.GetPly() != 5 {
		t.Fatalf("spectator saw the end as %v", final)
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
		return len(history.GetFrames()) == 6
	})
	if history.GetSession().GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON {
		t.Fatalf("history session status = %v", history.GetSession().GetStatus())
	}
	cells := []uint32{0, 4, 1, 5, 2}
	for ply, frame := range history.GetFrames() {
		if frame.GetPly() != uint32(ply) {
			t.Fatalf("frame %d is at ply %d", ply, frame.GetPly())
		}
		if ply == 0 {
			if frame.PlayerId != nil {
				t.Fatal("the starting position has no mover")
			}
			continue
		}
		mover := x.id
		mark := gamev1.Mark_MARK_X
		if ply%2 == 0 {
			mover, mark = o.id, gamev1.Mark_MARK_O
		}
		if frame.GetPlayerId() != mover || frame.GetPlayedAt() == nil {
			t.Fatalf("frame %d mover = %q, played_at = %v", ply, frame.GetPlayerId(), frame.GetPlayedAt())
		}
		if got := frame.GetTicTacToe().GetCells()[cells[ply-1]]; got != mark {
			t.Fatalf("frame %d cell %d = %v, want %v", ply, cells[ply-1], got, mark)
		}
		if marked := countMarks(frame.GetTicTacToe().GetCells()); marked != ply {
			t.Fatalf("frame %d holds %d marks", ply, marked)
		}
	}
	if winning := history.GetFrames()[5].GetTicTacToe().GetWinningCells(); len(winning) != 3 {
		t.Fatalf("final frame winning cells = %v", winning)
	}
}

func countMarks(cells []gamev1.Mark) int {
	marked := 0
	for _, cell := range cells {
		if cell != gamev1.Mark_MARK_UNSPECIFIED {
			marked++
		}
	}
	return marked
}

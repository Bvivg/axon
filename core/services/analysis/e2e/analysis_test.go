//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	analysisv1 "github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/analysis/v1/analysisv1connect"
	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

var scholar = []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"}

type player struct {
	id    string
	token string
}

func setting(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func gatewayURL() string { return setting("GATEWAY_URL", "http://gateway:8080") }

func client() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

func register(t *testing.T) player {
	t.Helper()
	auth := authv1connect.NewAuthServiceClient(client(), gatewayURL())
	registered, err := auth.Register(t.Context(), connect.NewRequest(&authv1.RegisterRequest{
		Email: "analysis-" + uuid.NewString()[:8] + "@axon.test", Password: "analysis-" + uuid.NewString(),
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

type socket struct {
	t    *testing.T
	conn *ws.Conn
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

func (s *socket) at(ply uint32) *gamev1.Session {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.t.Context(), 20*time.Second)
	defer cancel()
	for {
		raw, err := s.conn.Read(ctx)
		if err != nil {
			s.t.Fatalf("waiting for ply %d: %v", ply, err)
		}
		var event gamev1.SessionEvent
		if err := protojson.Unmarshal(raw, &event); err != nil {
			s.t.Fatal(err)
		}
		if event.GetError() != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
			s.t.Fatalf("game error at ply %d: %v", ply, event.GetError())
		}
		if session := event.GetSession(); session != nil && session.GetPly() == ply {
			return session
		}
	}
}

func matched(t *testing.T, games gamev1connect.GameServiceClient, a, b player) (string, player, player) {
	t.Helper()
	ctx := t.Context()
	for _, who := range []player{a, b} {
		if _, err := games.FindMatch(ctx, as(who, &gamev1.FindMatchRequest{Game: "chess", RequestId: uuid.NewString()})); err != nil {
			t.Fatal("find match:", err)
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		ticket, err := games.GetMatch(ctx, as(a, &gamev1.GetMatchRequest{Game: "chess"}))
		if err != nil {
			t.Fatal("get match:", err)
		}
		if id := ticket.Msg.GetTicket().GetSessionId(); ticket.Msg.GetTicket().GetStatus() == gamev1.MatchStatus_MATCH_STATUS_MATCHED && id != "" {
			game, err := games.GetSession(ctx, as(a, &gamev1.GetSessionRequest{SessionId: id}))
			if err != nil {
				t.Fatal("get session:", err)
			}
			if !game.Msg.GetSession().GetRated() {
				t.Fatalf("a quick match is unrated: %v", game.Msg.GetSession())
			}
			if game.Msg.GetSession().GetPlayerIds()[0] == b.id {
				a, b = b, a
			}
			return id, a, b
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the quick match")
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func play(t *testing.T, white, black player, id string, moves []string) *gamev1.Session {
	t.Helper()
	sockets := map[string]*socket{white.id: open(t, white, id), black.id: open(t, black, id)}
	for ply, uci := range moves {
		var mover *socket
		for _, s := range sockets {
			session := s.at(uint32(ply))
			if session.GetTurnUserId() != "" {
				mover = sockets[session.GetTurnUserId()]
			}
		}
		if mover == nil {
			t.Fatalf("nobody is to move at ply %d", ply)
		}
		mover.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_ChessMove{ChessMove: &gamev1.ChessMove{Ply: uint32(ply), Uci: uci}}})
	}
	return sockets[white.id].at(uint32(len(moves)))
}

func engineSeconds(t *testing.T) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, setting("ANALYSIS_METRICS_URL", "http://analysis:9095/metrics"), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client().Do(req)
	if err != nil {
		return "unknown (" + err.Error() + ")"
	}
	defer func() { _ = res.Body.Close() }()
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "analysis_duration_seconds_sum "); ok {
			return value + "s"
		}
	}
	return "unknown"
}

func TestScholarsMateBlunderIsFoundThroughTheGateway(t *testing.T) {
	ctx := t.Context()
	games := gamev1connect.NewGameServiceClient(client(), gatewayURL())
	analysis := analysisv1connect.NewAnalysisServiceClient(client(), gatewayURL())
	white, black := register(t), register(t)
	casual, err := games.CreateSession(ctx, as(white, &gamev1.CreateSessionRequest{Game: "chess", RequestId: uuid.NewString()}))
	if err != nil {
		t.Fatal("create:", err)
	}
	if _, err := games.JoinSession(ctx, as(black, &gamev1.JoinSessionRequest{SessionId: casual.Msg.GetSession().GetId()})); err != nil {
		t.Fatal("join:", err)
	}
	if final := play(t, white, black, casual.Msg.GetSession().GetId(), []string{"f2f3", "e7e5", "g2g4", "d8h4"}); final.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON {
		t.Fatalf("casual game = %v", final)
	}

	id, white, black := matched(t, games, white, black)
	if _, err := analysis.GetAnalysis(ctx, connect.NewRequest(&analysisv1.GetAnalysisRequest{SessionId: id})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous GetAnalysis = %v, want Unauthenticated", err)
	}
	if _, err := analysis.GetAnalysis(ctx, as(black, &analysisv1.GetAnalysisRequest{SessionId: id})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("GetAnalysis of a live game = %v, want NotFound", err)
	}

	final := play(t, white, black, id, scholar)
	if final.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || final.GetWinnerUserId() != white.id || final.GetChess().GetEnd() != gamev1.ChessEnd_CHESS_END_CHECKMATE {
		t.Fatalf("final session = %v", final)
	}
	finishedAt := time.Now()

	var report *analysisv1.GetAnalysisResponse
	deadline := time.Now().Add(2 * time.Minute)
	for {
		response, err := analysis.GetAnalysis(ctx, as(black, &analysisv1.GetAnalysisRequest{SessionId: id}))
		switch {
		case err == nil && response.Msg.GetStatus() == analysisv1.AnalysisStatus_ANALYSIS_STATUS_READY:
			report = response.Msg
		case err == nil && response.Msg.GetStatus() == analysisv1.AnalysisStatus_ANALYSIS_STATUS_FAILED:
			t.Fatalf("analysis failed: %v", response.Msg)
		case err != nil && connect.CodeOf(err) != connect.CodeNotFound:
			t.Fatal("GetAnalysis:", err)
		}
		if report != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the analysis")
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("%s at depth %d: ready %s after checkmate, engine time %s", report.GetEngine(), report.GetDepth(), time.Since(finishedAt).Round(time.Millisecond), engineSeconds(t))
	if _, err := analysis.GetAnalysis(ctx, as(black, &analysisv1.GetAnalysisRequest{SessionId: casual.Msg.GetSession().GetId()})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("GetAnalysis of a casual game = %v, want NotFound", err)
	}

	positions := report.GetPositions()
	if len(positions) != len(scholar)+1 || report.GetDepth() != 12 || !strings.HasPrefix(report.GetEngine(), "Stockfish") {
		t.Fatalf("report has %d positions at depth %d by %q", len(positions), report.GetDepth(), report.GetEngine())
	}
	for ply, position := range positions {
		if position.GetPly() != uint32(ply) {
			t.Fatalf("position %d is ply %d", ply, position.GetPly())
		}
		if ply > 0 && position.GetPlayedMove() != scholar[ply-1] {
			t.Fatalf("ply %d played %q", ply, position.GetPlayedMove())
		}
		mate := "-"
		if position.Mate != nil {
			mate = strconv.Itoa(int(position.GetMate()))
		}
		t.Logf("ply %d %-5s cp %6d mate %-2s best %-5s loss %5d %v", ply, position.GetPlayedMove(), position.GetCentipawns(), mate,
			position.GetBestMove(), position.GetLoss(), position.GetMoveClass())
	}
	if blunder := positions[6]; blunder.GetMoveClass() != analysisv1.MoveClass_MOVE_CLASS_BLUNDER || blunder.GetLoss() < 300 || blunder.GetMate() != 1 || blunder.GetBestMove() != "h5f7" {
		t.Fatalf("ply 6 (g8f6) = %v, want a blunder that allows mate in 1", blunder)
	}
	if mate := positions[len(positions)-1]; mate.Mate == nil || mate.GetMate() != 0 || mate.GetCentipawns() != 10000 || mate.BestMove != nil ||
		mate.GetMoveClass() != analysisv1.MoveClass_MOVE_CLASS_BEST {
		t.Fatalf("final position = %v, want white to have mated", mate)
	}
	if positions[0].PlayedMove != nil || positions[0].GetMoveClass() != analysisv1.MoveClass_MOVE_CLASS_UNSPECIFIED {
		t.Fatalf("start position = %v", positions[0])
	}
}

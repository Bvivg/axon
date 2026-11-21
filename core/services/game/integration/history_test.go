//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/server"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func shuffleGame(t *testing.T, ctx context.Context, rules session.Rules, plies int) session.Session {
	t.Helper()
	x, o := uuid.New(), uuid.New()
	state, err := rules.Games().NewState(quoridor.Key, []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())})
	if err != nil {
		t.Fatal(err)
	}
	game := session.Session{ID: uuid.New(), Game: quoridor.Key, Creator: x, Opponent: &o, State: state}
	cycle := [2][2]string{{"e2", "e1"}, {"e8", "e9"}}
	moves := make([]session.MoveRecord, plies)
	start := time.Now().UTC().Truncate(time.Second)
	for ply := range plies {
		player := x
		if ply%2 == 1 {
			player = o
		}
		data, err := json.Marshal(quoridor.MoveData{Move: cycle[ply%2][(ply/2)%2]})
		if err != nil {
			t.Fatal(err)
		}
		moves[ply] = session.MoveRecord{Ply: ply + 1, Player: player, Data: data, PlayedAt: start.Add(time.Duration(ply) * time.Millisecond)}
	}
	if game.State, err = rules.Replay(game, moves); err != nil {
		t.Fatal(err)
	}
	game.Revision = uint32(plies + 1)
	raw, err := json.Marshal(game.State)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO game.sessions (id, game, creator_id, opponent_id, request_id, state, updated_at, revision, partition)
		VALUES ($1, $2, $3, $4, $1, $5, now(), $6, 0)`, game.ID, game.Game, x, o, raw, game.Revision); err != nil {
		t.Fatal(err)
	}
	for _, move := range moves {
		if _, err := pool.Exec(ctx, `INSERT INTO game.moves (session_id, ply, player_id, data, played_at) VALUES ($1, $2, $3, $4, $5)`,
			game.ID, move.Ply, move.Player, string(move.Data), move.PlayedAt); err != nil {
			t.Fatal(err)
		}
	}
	return game
}

func history(ctx context.Context, h *server.Handler, user uuid.UUID, id uuid.UUID) (*gamev1.GetSessionHistoryResponse, error) {
	req := connect.NewRequest(&gamev1.GetSessionHistoryRequest{SessionId: id.String()})
	req.Header().Set(server.TestUserHeader, user.String())
	res, err := h.GetSessionHistory(ctx, req)
	if err != nil {
		return nil, err
	}
	return res.Msg, nil
}

func TestHistoryOfAGameDrawnByTheMoveLimitIsLinearAndCached(t *testing.T) {
	ctx := context.Background()
	games := engine.NewRegistry()
	if err := games.Register(quoridor.Definition()); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRules(games)
	store := session.NewStore(pool, games)
	game := shuffleGame(t, ctx, rules, quoridor.MaxPlies)
	if !game.State.MoveLimit || game.State.Status != engine.StatusDraw {
		t.Fatalf("a %d-ply shuffle ended as %q, limit %v", quoridor.MaxPlies, game.State.Status, game.State.MoveLimit)
	}
	stranger := uuid.New()
	h := server.New(server.Config{Store: store, TestHooks: true, History: server.HistoryLimits{Plies: server.DefaultHistoryPlies, Bytes: server.DefaultHistoryBytes, Entries: 4}})

	first, err := history(ctx, h, stranger, game.ID)
	if err != nil {
		t.Fatal(err)
	}
	frames := first.GetFrames()
	if len(frames) != quoridor.MaxPlies+1 || first.GetSession().GetStatus() != gamev1.SessionStatus_SESSION_STATUS_DRAW || !first.GetSession().GetMoveLimit() {
		t.Fatalf("history = %d frames, session %v", len(frames), first.GetSession())
	}
	for i, frame := range frames[:quoridor.MaxPlies] {
		if len(frame.GetQuoridor().GetMoves()) != 0 || (i > 0) != (frame.GetMove() != "") {
			t.Fatalf("frame %d = %d moves, move %q", i, len(frame.GetQuoridor().GetMoves()), frame.GetMove())
		}
	}
	if last := frames[quoridor.MaxPlies]; len(last.GetQuoridor().GetMoves()) != quoridor.MaxPlies || last.GetMove() != "e9" {
		t.Fatalf("last frame = %d moves, move %q", len(last.GetQuoridor().GetMoves()), last.GetMove())
	}
	size := proto.Size(first)
	t.Logf("history of %d plies: %d bytes", quoridor.MaxPlies, size)
	if size > 128<<10 {
		t.Fatalf("history of %d plies is %d bytes", quoridor.MaxPlies, size)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM game.moves WHERE session_id=$1`, game.ID); err != nil {
		t.Fatal(err)
	}
	cached, err := history(ctx, h, stranger, game.ID)
	if err != nil || len(cached.GetFrames()) != quoridor.MaxPlies+1 {
		t.Fatalf("history of a finished game after its moves left the database = %d frames, %v; want the cached replay", len(cached.GetFrames()), err)
	}
	if _, err := pool.Exec(ctx, `UPDATE game.sessions SET revision = revision + 1 WHERE id=$1`, game.ID); err != nil {
		t.Fatal(err)
	}
	if fresh, err := history(ctx, h, stranger, game.ID); err != nil || len(fresh.GetFrames()) != 1 {
		t.Fatalf("history after a new revision = %d frames, %v; want a fresh replay without the deleted moves", len(fresh.GetFrames()), err)
	}

	again := shuffleGame(t, ctx, rules, quoridor.MaxPlies)
	small := server.New(server.Config{Store: store, TestHooks: true, History: server.HistoryLimits{Plies: server.DefaultHistoryPlies, Bytes: 16 << 10}})
	if _, err := history(ctx, small, stranger, again.ID); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("history over the byte limit = %v, want %v", err, connect.CodeResourceExhausted)
	}
	short := server.New(server.Config{Store: store, TestHooks: true, History: server.HistoryLimits{Plies: 100, Bytes: server.DefaultHistoryBytes}})
	if _, err := history(ctx, short, stranger, again.ID); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("history over the ply limit = %v, want %v", err, connect.CodeResourceExhausted)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := history(cancelled, h, stranger, again.ID); err == nil {
		t.Fatal("a cancelled request built the history")
	}
}

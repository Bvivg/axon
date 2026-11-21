package server

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

func historyRules(t *testing.T, maxPlies int) session.Rules {
	t.Helper()
	games := engine.NewRegistry()
	shuffle := quoridor.Definition()
	shuffle.MaxPlies = maxPlies
	for _, def := range []engine.Definition{shuffle, chess.Definition()} {
		if err := games.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return session.NewRules(games)
}

func shuffled(t *testing.T, r session.Rules, plies int) (session.Session, []session.MoveRecord) {
	t.Helper()
	x, o := uuid.New(), uuid.New()
	state, err := r.Games().NewState(quoridor.Key, []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())})
	if err != nil {
		t.Fatal(err)
	}
	game := session.Session{ID: uuid.New(), Game: quoridor.Key, Creator: x, Opponent: &o, State: state}
	cycle := [2][2]string{{"e2", "e1"}, {"e8", "e9"}}
	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	moves := make([]session.MoveRecord, plies)
	for ply := range plies {
		player := x
		if ply%2 == 1 {
			player = o
		}
		data, err := json.Marshal(quoridor.MoveData{Move: cycle[ply%2][(ply/2)%2]})
		if err != nil {
			t.Fatal(err)
		}
		moves[ply] = session.MoveRecord{Ply: ply + 1, Player: player, Data: data, PlayedAt: start.Add(time.Duration(ply) * time.Second)}
	}
	return game, moves
}

func framesSize(frames []*gamev1.HistoryFrame) int {
	return proto.Size(&gamev1.GetSessionHistoryResponse{Frames: frames})
}

func TestHistoryGrowsLinearlyWithTheGame(t *testing.T) {
	r := historyRules(t, 0)
	sizes := map[int]int{}
	for _, plies := range []int{300, 600} {
		game, moves := shuffled(t, r, plies)
		frames, err := buildHistory(t.Context(), r, game, moves, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(frames) != plies+1 {
			t.Fatalf("%d plies gave %d frames", plies, len(frames))
		}
		for i, frame := range frames[:plies] {
			if len(frame.GetQuoridor().GetMoves()) != 0 {
				t.Fatalf("frame %d of %d repeats %d earlier moves", i, plies, len(frame.GetQuoridor().GetMoves()))
			}
		}
		last := frames[plies]
		if len(last.GetQuoridor().GetMoves()) != plies || last.GetMove() != "e9" || last.GetPlayerId() != game.Opponent.String() {
			t.Fatalf("last frame = %d moves, move %q, player %q", len(last.GetQuoridor().GetMoves()), last.GetMove(), last.GetPlayerId())
		}
		if frames[0].Move != nil || frames[1].GetMove() != "e2" || frames[2].GetMove() != "e8" || !frames[1].GetPlayedAt().AsTime().Equal(moves[0].PlayedAt) {
			t.Fatalf("first frames = %v", frames[:3])
		}
		sizes[plies] = framesSize(frames)
	}
	if ratio := float64(sizes[600]) / float64(sizes[300]); ratio > 2.2 {
		t.Fatalf("history of 600 plies is %.1f times the history of 300 (%d vs %d bytes), want linear growth", ratio, sizes[600], sizes[300])
	}
}

func TestChessHistoryNamesEachMoveOnce(t *testing.T) {
	r := historyRules(t, 0)
	x, o := uuid.New(), uuid.New()
	players := []engine.PlayerID{engine.PlayerID(x.String()), engine.PlayerID(o.String())}
	state, err := r.Games().NewState(chess.Key, players)
	if err != nil {
		t.Fatal(err)
	}
	game := session.Session{ID: uuid.New(), Game: chess.Key, Creator: x, Opponent: &o, State: state}
	var moves []session.MoveRecord
	for ply, uci := range []string{"e2e4", "e7e5", "f1c4", "b8c6", "d1h5", "g8f6", "h5f7"} {
		player := x
		if ply%2 == 1 {
			player = o
		}
		data, err := json.Marshal(chess.MoveData{UCI: uci})
		if err != nil {
			t.Fatal(err)
		}
		moves = append(moves, session.MoveRecord{Ply: ply + 1, Player: player, Data: data})
	}
	frames, err := buildHistory(t.Context(), r, game, moves, 0)
	if err != nil {
		t.Fatal(err)
	}
	var named []string
	for i, frame := range frames {
		if i > 0 {
			named = append(named, frame.GetMove())
		}
		if i < len(moves) && len(frame.GetChess().GetSan()) != 0 {
			t.Fatalf("frame %d repeats the move list", i)
		}
		if len(frame.GetChess().GetLegalMoves()) != 0 {
			t.Fatalf("frame %d offers legal moves", i)
		}
	}
	want := []string{"e4", "e5", "Bc4", "Nc6", "Qh5", "Nf6", "Qxf7#"}
	if !slices.Equal(named, want) || !slices.Equal(frames[len(moves)].GetChess().GetSan(), want) || frames[len(moves)].GetChess().GetLastMove() != "h5f7" {
		t.Fatalf("moves = %v, last list %v", named, frames[len(moves)].GetChess().GetSan())
	}
}

func TestHistoryRefusesGamesLongerThanTheLimit(t *testing.T) {
	r := historyRules(t, 0)
	game, moves := shuffled(t, r, 30)
	if _, err := buildHistory(t.Context(), r, game, moves, 29); !errors.Is(err, errHistoryTooLong) {
		t.Fatalf("history over the limit = %v, want %v", err, errHistoryTooLong)
	}
	if _, err := buildHistory(t.Context(), r, game, moves, 30); err != nil {
		t.Fatalf("history at the limit = %v", err)
	}
}

func TestHistoryStopsWhenTheCallerLeaves(t *testing.T) {
	r := historyRules(t, 0)
	game, moves := shuffled(t, r, 30)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := buildHistory(ctx, r, game, moves, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("history after a cancel = %v, want %v", err, context.Canceled)
	}
}

func TestHistoryCacheKeepsTheNewestFinishedGames(t *testing.T) {
	cache := newHistoryCache(2)
	first, second, third := uuid.New(), uuid.New(), uuid.New()
	frames := []*gamev1.HistoryFrame{{Ply: 0}}
	cache.put(first, 3, frames)
	cache.put(second, 5, frames)
	if got, ok := cache.get(first, 3); !ok || len(got) != 1 {
		t.Fatal("a cached game is missing")
	}
	if _, ok := cache.get(first, 4); ok {
		t.Fatal("a cached game matched another revision")
	}
	cache.put(third, 1, frames)
	if _, ok := cache.get(second, 5); ok {
		t.Fatal("the least recently used game survived the eviction")
	}
	if _, ok := cache.get(first, 3); !ok {
		t.Fatal("the recently read game was evicted")
	}
	if _, ok := cache.get(third, 1); !ok {
		t.Fatal("the newest game is missing")
	}
	if _, ok := cache.revision(third); !ok {
		t.Fatal("the cached revision is missing")
	}
	off := newHistoryCache(0)
	off.put(first, 1, frames)
	if _, ok := off.get(first, 1); ok {
		t.Fatal("a disabled cache kept a game")
	}
}

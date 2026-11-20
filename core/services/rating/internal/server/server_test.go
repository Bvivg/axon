package server_test

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/server"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

var (
	alice = uuid.MustParse("1d6f2f43-9b35-4a6f-8f43-2a1c9d0e7b11")
	bob   = uuid.MustParse("2e7a3b54-ac46-4b70-9054-3b2dae1f8c22")
)

type board struct {
	entries []leaderboard.Entry
	err     error
	asked   [2]int
}

func (b *board) Top(_ context.Context, _ string, offset, limit int) ([]leaderboard.Entry, int64, error) {
	b.asked = [2]int{offset, limit}
	return b.entries, int64(len(b.entries)) + int64(offset), b.err
}

type standings struct {
	players map[uuid.UUID]ledger.Player
	err     error
}

func (s standings) Players(context.Context, string, []uuid.UUID) (map[uuid.UUID]ledger.Player, error) {
	return s.players, s.err
}

func (s standings) Top(context.Context, string, int, int) ([]ledger.Player, int64, error) {
	if s.err != nil {
		return nil, 0, s.err
	}
	return []ledger.Player{s.players[bob]}, 1, nil
}

func detailed(user uuid.UUID, rating float64, games int) ledger.Player {
	return ledger.Player{User: user, Rating: glicko.Rating{Rating: rating, Deviation: 120.5, Volatility: 0.06}, Games: games, Wins: games}
}

func TestGetLeaderboard(t *testing.T) {
	stored := standings{players: map[uuid.UUID]ledger.Player{alice: detailed(alice, 1500.4, 3), bob: detailed(bob, 1410, 2)}}
	tests := []struct {
		name      string
		request   *ratingv1.GetLeaderboardRequest
		board     *board
		standings standings
		code      connect.Code
		want      []*ratingv1.LeaderboardEntry
		total     uint32
		asked     [2]int
	}{
		{
			name:      "first page with details from postgres",
			request:   &ratingv1.GetLeaderboardRequest{Game: "chess"},
			board:     &board{entries: []leaderboard.Entry{{User: alice, Score: 1500.6}, {User: bob, Score: 1410}}},
			standings: stored,
			want: []*ratingv1.LeaderboardEntry{
				{Rank: 1, UserId: alice.String(), Rating: 1501, Deviation: 120.5, Games: 3, Wins: 3},
				{Rank: 2, UserId: bob.String(), Rating: 1410, Deviation: 120.5, Games: 2, Wins: 2},
			},
			total: 2,
			asked: [2]int{0, server.DefaultLimit},
		},
		{
			name:      "ranks continue from the offset",
			request:   &ratingv1.GetLeaderboardRequest{Game: "chess", Offset: 10, Limit: 1},
			board:     &board{entries: []leaderboard.Entry{{User: bob, Score: 1410}}},
			standings: stored,
			want:      []*ratingv1.LeaderboardEntry{{Rank: 11, UserId: bob.String(), Rating: 1410, Deviation: 120.5, Games: 2, Wins: 2}},
			total:     11,
			asked:     [2]int{10, 1},
		},
		{
			name:      "falls back to postgres without redis",
			request:   &ratingv1.GetLeaderboardRequest{Game: "chess"},
			board:     &board{err: errors.New("redis down")},
			standings: stored,
			want:      []*ratingv1.LeaderboardEntry{{Rank: 1, UserId: bob.String(), Rating: 1410, Deviation: 120.5, Games: 2, Wins: 2}},
			total:     1,
			asked:     [2]int{0, server.DefaultLimit},
		},
		{name: "unknown game key", request: &ratingv1.GetLeaderboardRequest{Game: "Chess; DROP"}, board: &board{}, code: connect.CodeInvalidArgument},
		{name: "limit too large", request: &ratingv1.GetLeaderboardRequest{Game: "chess", Limit: 101}, board: &board{}, code: connect.CodeInvalidArgument},
		{name: "offset too deep", request: &ratingv1.GetLeaderboardRequest{Game: "chess", Offset: 10_001}, board: &board{}, code: connect.CodeInvalidArgument},
		{
			name:      "both stores down",
			request:   &ratingv1.GetLeaderboardRequest{Game: "chess"},
			board:     &board{err: errors.New("redis down")},
			standings: standings{err: errors.New("postgres down")},
			code:      connect.CodeUnavailable,
			asked:     [2]int{0, server.DefaultLimit},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := server.New(tt.board, tt.standings, logger.Discard())
			response, err := handler.GetLeaderboard(context.Background(), connect.NewRequest(tt.request))
			if tt.code != 0 {
				if connect.CodeOf(err) != tt.code {
					t.Fatalf("code = %v, want %v (%v)", connect.CodeOf(err), tt.code, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.board.asked != tt.asked || response.Msg.Total != tt.total || len(response.Msg.Entries) != len(tt.want) {
				t.Fatalf("asked %v, response %v", tt.board.asked, response.Msg)
			}
			for i, entry := range response.Msg.Entries {
				want := tt.want[i]
				if entry.Rank != want.Rank || entry.UserId != want.UserId || entry.Rating != want.Rating || entry.Deviation != want.Deviation ||
					entry.Games != want.Games || entry.Wins != want.Wins {
					t.Fatalf("entry %d = %v, want %v", i, entry, want)
				}
			}
		})
	}
}

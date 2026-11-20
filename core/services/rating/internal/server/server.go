package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
	MaxOffset    = 10_000
)

type Board interface {
	Top(ctx context.Context, game string, offset, limit int) ([]leaderboard.Entry, int64, error)
}

type Standings interface {
	Players(ctx context.Context, game string, users []uuid.UUID) (map[uuid.UUID]ledger.Player, error)
	Top(ctx context.Context, game string, offset, limit int) ([]ledger.Player, int64, error)
}

type Handler struct {
	board     Board
	standings Standings
	log       *slog.Logger
}

var _ ratingv1connect.RatingServiceHandler = (*Handler)(nil)

func New(board Board, standings Standings, log *slog.Logger) *Handler {
	return &Handler{board: board, standings: standings, log: log}
}

var errUnavailable = errors.New("leaderboard is unavailable")

func (h *Handler) GetLeaderboard(ctx context.Context, req *connect.Request[ratingv1.GetLeaderboardRequest]) (*connect.Response[ratingv1.GetLeaderboardResponse], error) {
	game := req.Msg.GetGame()
	if !ledger.ValidGame(game) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("game must be a known game key"))
	}
	limit, offset := int(req.Msg.GetLimit()), int(req.Msg.GetOffset())
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit || offset > MaxOffset {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("limit must be at most 100 and offset at most 10000"))
	}
	players, total, err := h.ranked(ctx, game, offset, limit)
	if err != nil {
		h.log.ErrorContext(ctx, "leaderboard read failed", "game", game, "error", err)
		return nil, connect.NewError(connect.CodeUnavailable, errUnavailable)
	}
	response := &ratingv1.GetLeaderboardResponse{Total: uint32(total)}
	for i, player := range players {
		response.Entries = append(response.Entries, &ratingv1.LeaderboardEntry{
			Rank: uint32(offset + i + 1), UserId: player.User.String(), Rating: uint32(max(ledger.Rounded(player.Rating.Rating), 0)),
			Deviation: player.Rating.Deviation, Games: uint32(player.Games), Wins: uint32(player.Wins), Losses: uint32(player.Losses), Draws: uint32(player.Draws),
		})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) ranked(ctx context.Context, game string, offset, limit int) ([]ledger.Player, int64, error) {
	entries, total, err := h.board.Top(ctx, game, offset, limit)
	if err != nil {
		h.log.WarnContext(ctx, "leaderboard cache unavailable, reading postgres", "game", game, "error", err)
		return h.standings.Top(ctx, game, offset, limit)
	}
	users := make([]uuid.UUID, len(entries))
	for i, entry := range entries {
		users[i] = entry.User
	}
	details, err := h.standings.Players(ctx, game, users)
	if err != nil {
		return nil, 0, err
	}
	players := make([]ledger.Player, 0, len(entries))
	for _, entry := range entries {
		player, ok := details[entry.User]
		if !ok {
			player = ledger.Newcomer(entry.User)
		}
		player.Rating.Rating = entry.Score
		players = append(players, player)
	}
	return players, total, nil
}

package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/services/game/internal/tournament"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

var _ tournament.Games = (*Handler)(nil)

func startRequest(game tournament.Game) *gamev1.StartMatchRequest {
	id := game.Tournament.String()
	return &gamev1.StartMatchRequest{
		SessionId: game.Session.String(), Game: string(game.Game), CreatorId: game.Players[0].String(), OpponentId: game.Players[1].String(),
		TournamentId: &id,
	}
}

func (h *Handler) StartTournamentGame(ctx context.Context, game tournament.Game) error {
	return h.onOwnerOf(game.Session, func() error {
		_, err := h.owner.StartTournament(ctx, game.Session, game.Game, game.Tournament, game.Players[0], game.Players[1])
		return err
	}, func(peer gamev1connect.GameOwnerServiceClient) error {
		_, err := peer.StartMatch(ctx, connect.NewRequest(startRequest(game)))
		return err
	})
}

func (h *Handler) ForfeitTournamentGame(ctx context.Context, game tournament.Game) error {
	return h.onOwnerOf(game.Session, func() error {
		return h.forfeit(ctx, game.Session, game.Game, game.Tournament, game.Players)
	}, func(peer gamev1connect.GameOwnerServiceClient) error {
		_, err := peer.ForfeitSession(ctx, connect.NewRequest(&gamev1.ForfeitSessionRequest{Session: startRequest(game)}))
		return err
	})
}

func (h *Handler) AbortTournamentGame(ctx context.Context, id uuid.UUID) error {
	return h.onOwnerOf(id, func() error {
		_, err := h.owner.Abort(ctx, id)
		return err
	}, func(peer gamev1connect.GameOwnerServiceClient) error {
		_, err := peer.AbortSession(ctx, connect.NewRequest(&gamev1.AbortSessionRequest{SessionId: id.String()}))
		return err
	})
}

func (h *Handler) forfeit(ctx context.Context, id uuid.UUID, key engine.Key, tournament uuid.UUID, players [2]uuid.UUID) error {
	if _, err := h.owner.StartTournament(ctx, id, key, tournament, players[0], players[1]); err != nil {
		return err
	}
	_, err := h.owner.Forfeit(ctx, id)
	if errors.Is(err, session.ErrNotPlaying) || errors.Is(err, session.ErrNotFound) {
		return nil
	}
	return err
}

func (h *Handler) ForfeitSession(ctx context.Context, req *connect.Request[gamev1.ForfeitSessionRequest]) (*connect.Response[gamev1.ForfeitSessionResponse], error) {
	p, err := parsePairing(req.Msg.GetSession())
	if err != nil || p.tournament == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid match"))
	}
	if err := ownerError(h.forfeit(ctx, p.session, p.game, *p.tournament, [2]uuid.UUID{p.creator, p.opponent})); err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.ForfeitSessionResponse{}), nil
}

func (h *Handler) AbortSession(ctx context.Context, req *connect.Request[gamev1.AbortSessionRequest]) (*connect.Response[gamev1.AbortSessionResponse], error) {
	id, err := sessionID(req.Msg.GetSessionId())
	if err != nil {
		return nil, err
	}
	game, err := h.owner.Abort(ctx, id)
	if err != nil {
		return nil, ownerError(err)
	}
	cancelled := aborted(game)
	return connect.NewResponse(&gamev1.AbortSessionResponse{Cancelled: &cancelled}), nil
}

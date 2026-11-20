package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/matchmaking"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

const maxRatingLookups = 50

func ticketView(game string, ticket session.Ticket, err error) (*gamev1.MatchTicket, error) {
	switch {
	case errors.Is(err, session.ErrNotSearching):
		return &gamev1.MatchTicket{Game: game, Status: gamev1.MatchStatus_MATCH_STATUS_IDLE}, nil
	case errors.Is(err, engine.ErrUnknownGame):
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, errors.New("matchmaking unavailable"))
	}
	out := &gamev1.MatchTicket{
		Game: game, Status: gamev1.MatchStatus_MATCH_STATUS_SEARCHING, Rating: uint32(ticket.Rating),
		JoinedAt: timestamppb.New(ticket.JoinedAt), SampledAt: timestamppb.New(ticket.Now), WaitMs: uint32(ticket.Wait.Milliseconds()),
	}
	if window := ticket.Window(); window != matchmaking.Unlimited {
		out.Window = proto.Uint32(uint32(window))
	}
	if ticket.Session != nil {
		out.Status = gamev1.MatchStatus_MATCH_STATUS_MATCHED
		out.SessionId = proto.String(ticket.Session.String())
	}
	return out, nil
}

func (h *Handler) FindMatch(ctx context.Context, req *connect.Request[gamev1.FindMatchRequest]) (*connect.Response[gamev1.FindMatchResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	request, err := uuid.Parse(req.Msg.RequestId)
	if err != nil || request == uuid.Nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid request ID"))
	}
	found, err := h.store.FindMatch(ctx, user, request, engine.Key(req.Msg.Game))
	ticket, err := ticketView(req.Msg.Game, found, err)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.FindMatchResponse{Ticket: ticket}), nil
}

func (h *Handler) CancelMatch(ctx context.Context, req *connect.Request[gamev1.CancelMatchRequest]) (*connect.Response[gamev1.CancelMatchResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	found, err := h.store.CancelMatch(ctx, user, engine.Key(req.Msg.Game))
	ticket, err := ticketView(req.Msg.Game, found, err)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.CancelMatchResponse{Ticket: ticket}), nil
}

func (h *Handler) GetMatch(ctx context.Context, req *connect.Request[gamev1.GetMatchRequest]) (*connect.Response[gamev1.GetMatchResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	found, err := h.store.Ticket(ctx, user, engine.Key(req.Msg.Game))
	ticket, err := ticketView(req.Msg.Game, found, err)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&gamev1.GetMatchResponse{Ticket: ticket}), nil
}

func (h *Handler) GetRatings(ctx context.Context, req *connect.Request[gamev1.GetRatingsRequest]) (*connect.Response[gamev1.GetRatingsResponse], error) {
	if _, err := h.actor(req.Header()); err != nil {
		return nil, err
	}
	if len(req.Msg.UserIds) > maxRatingLookups {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("too many players"))
	}
	users := make([]uuid.UUID, 0, len(req.Msg.UserIds))
	for _, raw := range req.Msg.UserIds {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid player"))
		}
		users = append(users, id)
	}
	ratings, err := h.store.Ratings(ctx, engine.Key(req.Msg.Game), users)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("ratings unavailable"))
	}
	response := &gamev1.GetRatingsResponse{}
	for _, user := range users {
		item := ratings[user]
		response.Ratings = append(response.Ratings, &gamev1.Rating{
			UserId: user.String(), Rating: uint32(item.Rating), Games: uint32(item.Games),
			Wins: uint32(item.Wins), Losses: uint32(item.Losses), Draws: uint32(item.Draws),
		})
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) RunMatchmaking(ctx context.Context, log *slog.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		created, err := h.store.MatchRound(ctx)
		if err != nil && ctx.Err() == nil {
			log.ErrorContext(ctx, "match players", "error", err)
		}
		for _, item := range created {
			log.InfoContext(ctx, "players matched", "session_id", item.ID, "game", item.Game)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

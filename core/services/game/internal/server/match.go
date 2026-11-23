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
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
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
	table, err := tableOf(engine.Key(req.Msg.Game), req.Msg.Seats, nil, req.Msg.Options)
	if err != nil {
		return nil, err
	}
	found, err := h.store.FindMatch(ctx, user, request, engine.Key(req.Msg.Game), table)
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
		if _, err := h.store.MatchRound(ctx); err != nil && ctx.Err() == nil {
			log.ErrorContext(ctx, "match players", "error", err)
		}
		pending, err := h.store.Pending(ctx)
		if err != nil && ctx.Err() == nil {
			log.ErrorContext(ctx, "load pending matches", "error", err)
		}
		for _, item := range pending {
			if err := h.startMatch(ctx, item); err != nil {
				if ctx.Err() == nil {
					log.WarnContext(ctx, "start matched game", "session_id", item.ID, "error", err)
				}
				continue
			}
			if err := h.store.Started(ctx, item); err != nil {
				log.ErrorContext(ctx, "mark match started", "session_id", item.ID, "error", err)
				continue
			}
			log.InfoContext(ctx, "players matched", "session_id", item.ID, "game", item.Game)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handler) onOwnerOf(id uuid.UUID, local func() error, remote func(gamev1connect.GameOwnerServiceClient) error) error {
	if h.owner.Serves(id) {
		return local()
	}
	address, ok := h.routes.Address(h.owner.PartitionOf(id))
	if !ok || address == h.self {
		return owner.ErrNotOwner
	}
	return remote(h.peers.owner(address))
}

func (h *Handler) startMatch(ctx context.Context, item session.Pairing) error {
	return h.onOwnerOf(item.ID, func() error {
		_, err := h.owner.Start(ctx, item.ID, item.Game, item.Players, item.Table, !cheating(item.Game, item.Table.Options))
		return err
	}, func(peer gamev1connect.GameOwnerServiceClient) error {
		request := &gamev1.StartMatchRequest{
			SessionId: item.ID.String(), Game: string(item.Game), CreatorId: item.Players[0].String(), OpponentId: item.Players[1].String(),
		}
		if !item.Plain() {
			request.PlayerIds = playerIDs(item.Players)
			request.Seats = proto.Uint32(uint32(len(item.Players)))
			request.OptionsJson = item.Table.Options
		}
		_, err := peer.StartMatch(ctx, connect.NewRequest(request))
		return err
	})
}

type pairing struct {
	session, creator, opponent uuid.UUID
	tournament                 *uuid.UUID
	game                       engine.Key
	players                    []uuid.UUID
	table                      session.Table
}

func parsePairing(req *gamev1.StartMatchRequest) (pairing, error) {
	invalid := connect.NewError(connect.CodeInvalidArgument, errors.New("invalid match"))
	if req == nil {
		return pairing{}, invalid
	}
	var ids [3]uuid.UUID
	for i, raw := range []string{req.SessionId, req.CreatorId, req.OpponentId} {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			return pairing{}, invalid
		}
		ids[i] = id
	}
	players, ok := pairingPlayers(req, ids[1], ids[2])
	if !ok {
		return pairing{}, invalid
	}
	out := pairing{session: ids[0], creator: ids[1], opponent: ids[2], game: engine.Key(req.Game), players: players}
	if len(req.GetOptionsJson()) > 0 || len(players) > session.DefaultSeats {
		out.table = session.Table{Seats: len(players), Options: req.GetOptionsJson()}
	}
	if req.TournamentId != nil {
		tournament, err := uuid.Parse(req.GetTournamentId())
		if err != nil || tournament == uuid.Nil {
			return pairing{}, invalid
		}
		out.tournament = &tournament
	}
	return out, nil
}

func (h *Handler) startPairing(ctx context.Context, p pairing) error {
	if p.tournament != nil {
		_, err := h.owner.StartTournament(ctx, p.session, p.game, *p.tournament, p.creator, p.opponent)
		return err
	}
	_, err := h.owner.Start(ctx, p.session, p.game, p.players, p.table, !cheating(p.game, p.table.Options))
	return err
}

func ownerError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, owner.ErrNotOwner):
		return errMoved
	case errors.Is(err, engine.ErrUnknownGame):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	default:
		return connect.NewError(connect.CodeInternal, errors.New("could not update the game"))
	}
}

func (h *Handler) StartMatch(ctx context.Context, req *connect.Request[gamev1.StartMatchRequest]) (*connect.Response[gamev1.StartMatchResponse], error) {
	p, err := parsePairing(req.Msg)
	if err != nil {
		return nil, err
	}
	err = h.startPairing(ctx, p)
	switch {
	case errors.Is(err, owner.ErrNotOwner):
		return nil, errMoved
	case errors.Is(err, engine.ErrUnknownGame):
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not start the game"))
	}
	return connect.NewResponse(&gamev1.StartMatchResponse{}), nil
}

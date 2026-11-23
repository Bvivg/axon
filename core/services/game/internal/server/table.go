package server

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
)

const openTablesLimit = 50

var errInvalidTable = connect.NewError(connect.CodeInvalidArgument, errors.New("invalid table settings"))

var (
	durakModes = map[gamev1.DurakMode]durak.Mode{
		gamev1.DurakMode_DURAK_MODE_UNSPECIFIED: durak.ModeThrowIn, gamev1.DurakMode_DURAK_MODE_THROW_IN: durak.ModeThrowIn, gamev1.DurakMode_DURAK_MODE_TRANSFER: durak.ModeTransfer,
	}
	durakThrowIn = map[gamev1.DurakThrowIn]durak.ThrowIn{
		gamev1.DurakThrowIn_DURAK_THROW_IN_UNSPECIFIED: durak.ThrowInAll, gamev1.DurakThrowIn_DURAK_THROW_IN_ALL: durak.ThrowInAll, gamev1.DurakThrowIn_DURAK_THROW_IN_NEIGHBORS: durak.ThrowInNeighbors,
	}
)

func tableOf(game engine.Key, seats, bots *uint32, options *gamev1.GameOptions) (session.Table, error) {
	table := session.Table{}
	if seats != nil {
		table.Seats = int(*seats)
	}
	if bots != nil {
		table.Bots = int(*bots)
	}
	if seats != nil && (table.Seats < session.DefaultSeats || table.Seats > durak.MaxSeats) {
		return session.Table{}, errInvalidTable
	}
	switch value := options.GetGame().(type) {
	case nil:
		if game != durak.Key && table.Seats == session.DefaultSeats && table.Bots == 0 {
			return session.Table{}, nil
		}
		if game == durak.Key && (table.Seats > session.DefaultSeats || table.Bots > 0) {
			return durakOptions(table, &gamev1.DurakOptions{Draw: true})
		}
		return table, nil
	case *gamev1.GameOptions_Durak:
		if game != durak.Key {
			return session.Table{}, errInvalidTable
		}
		return durakOptions(table, value.Durak)
	default:
		return session.Table{}, errInvalidTable
	}
}

func durakOptions(table session.Table, in *gamev1.DurakOptions) (session.Table, error) {
	mode, okMode := durakModes[in.GetMode()]
	throwIn, okThrowIn := durakThrowIn[in.GetThrowIn()]
	if !okMode || !okThrowIn {
		return session.Table{}, errInvalidTable
	}
	table.Seats = cmp.Or(table.Seats, session.DefaultSeats)
	raw, err := json.Marshal(durak.Options{
		Mode: mode, Cheats: in.GetCheats(), Deck: cmp.Or(int(in.GetDeck()), durak.DeckSize), ThrowIn: throwIn, Draw: in.GetDraw(),
		FirstBoutLimit: cmp.Or(int(in.GetFirstBoutLimit()), durak.HandSize), Seats: table.Seats,
	})
	if err != nil {
		return session.Table{}, errInvalidTable
	}
	table.Options = raw
	return table, nil
}

func optionsProto(game engine.Key, raw json.RawMessage) *gamev1.GameOptions {
	if game != durak.Key || len(raw) == 0 {
		return nil
	}
	var in durak.Options
	if json.Unmarshal(raw, &in) != nil {
		return nil
	}
	out := &gamev1.DurakOptions{Cheats: in.Cheats, Deck: uint32(in.Deck), Draw: in.Draw, FirstBoutLimit: uint32(in.FirstBoutLimit)}
	for mode, name := range durakModes {
		if name == in.Mode && mode != gamev1.DurakMode_DURAK_MODE_UNSPECIFIED {
			out.Mode = mode
		}
	}
	for throwIn, name := range durakThrowIn {
		if name == in.ThrowIn && throwIn != gamev1.DurakThrowIn_DURAK_THROW_IN_UNSPECIFIED {
			out.ThrowIn = throwIn
		}
	}
	return &gamev1.GameOptions{Game: &gamev1.GameOptions_Durak{Durak: out}}
}

func cheating(game engine.Key, raw json.RawMessage) bool {
	options := optionsProto(game, raw)
	return options != nil && options.GetDurak().GetCheats()
}

func seating(item session.Session) []*gamev1.SessionSeat {
	players := item.Players()
	places := item.Places()
	out := make([]*gamev1.SessionSeat, item.Capacity())
	for seat := range out {
		entry := &gamev1.SessionSeat{Seat: uint32(seat), Left: slices.Contains(item.Left, seat)}
		if seat < len(players) {
			entry.UserId = proto.String(players[seat].String())
			entry.Bot = session.IsBot(players[seat])
		}
		switch {
		case seat < len(places):
			entry.Place = proto.Uint32(uint32(places[seat]))
		case item.Out(seat):
			entry.Place = proto.Uint32(uint32(item.State.Places[seat]))
		}
		out[seat] = entry
	}
	return out
}

func (h *Handler) ListOpenTables(ctx context.Context, req *connect.Request[gamev1.ListOpenTablesRequest]) (*connect.Response[gamev1.ListOpenTablesResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	if req.Msg.Game != "" {
		if _, err := h.store.Games().Get(engine.Key(req.Msg.Game)); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown game"))
		}
	}
	items, err := h.store.Replica().OpenTables(ctx, engine.Key(req.Msg.Game), openTablesLimit)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("could not load tables"))
	}
	response := &gamev1.ListOpenTablesResponse{}
	for _, item := range items {
		state, err := h.result(item, user, nil)
		if err != nil {
			return nil, err
		}
		response.Sessions = append(response.Sessions, state)
	}
	return connect.NewResponse(response), nil
}

func (h *Handler) FillWithBots(ctx context.Context, req *connect.Request[gamev1.FillWithBotsRequest]) (*connect.Response[gamev1.FillWithBotsResponse], error) {
	user, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	id, err := sessionID(req.Msg.SessionId)
	if err != nil {
		return nil, err
	}
	return onOwner(h, ctx, id, req, func() (*connect.Response[gamev1.FillWithBotsResponse], error) {
		item, err := h.owner.FillWithBots(ctx, id, user)
		state, err := h.respond(ctx, item, user, err)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&gamev1.FillWithBotsResponse{Session: state}), nil
	}, gamev1connect.GameServiceClient.FillWithBots)
}

func pairingPlayers(req *gamev1.StartMatchRequest, creator, opponent uuid.UUID) ([]uuid.UUID, bool) {
	if len(req.GetPlayerIds()) == 0 {
		return []uuid.UUID{creator, opponent}, true
	}
	players := make([]uuid.UUID, 0, len(req.GetPlayerIds()))
	for _, raw := range req.GetPlayerIds() {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil || slices.Contains(players, id) {
			return nil, false
		}
		players = append(players, id)
	}
	return players, len(players) >= session.DefaultSeats && players[0] == creator && players[1] == opponent
}

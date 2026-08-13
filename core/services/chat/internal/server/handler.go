package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/identity"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

type Names interface {
	DisplayName(ctx context.Context, accessToken string) string
}

type Handler struct {
	svc      *service.Service
	verifier *authn.Verifier
	names    Names
	log      *slog.Logger
}

var _ chatv1connect.ChatServiceHandler = (*Handler)(nil)

type Config struct {
	Service  *service.Service
	Verifier *authn.Verifier
	Names    Names
	Logger   *slog.Logger
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Service == nil:
		return nil, errors.New("server: service is required")
	case cfg.Verifier == nil:
		return nil, errors.New("server: token verifier is required")
	case cfg.Names == nil:
		return nil, errors.New("server: name resolver is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}

	return &Handler{
		svc:      cfg.Service,
		verifier: cfg.Verifier,
		names:    cfg.Names,
		log:      cfg.Logger,
	}, nil
}

func (h *Handler) CreateRoom(
	ctx context.Context,
	req *connect.Request[chatv1.CreateRoomRequest],
) (*connect.Response[chatv1.CreateRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	room, err := h.svc.CreateRoom(ctx, service.CreateRoomInput{
		Name:        req.Msg.GetName(),
		UserID:      claims.UserID,
		DisplayName: h.names.DisplayName(ctx, identity.BearerFromHeader(req.Header())),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&chatv1.CreateRoomResponse{Room: toProtoRoom(room)}), nil
}

func (h *Handler) ListRooms(
	ctx context.Context,
	req *connect.Request[chatv1.ListRoomsRequest],
) (*connect.Response[chatv1.ListRoomsResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	rooms, err := h.svc.ListRooms(ctx, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	out := make([]*chatv1.Room, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, toProtoRoom(room))
	}

	return connect.NewResponse(&chatv1.ListRoomsResponse{Rooms: out}), nil
}

func (h *Handler) GetRoom(
	ctx context.Context,
	req *connect.Request[chatv1.GetRoomRequest],
) (*connect.Response[chatv1.GetRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	view, err := h.svc.GetRoom(ctx, roomID, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	members := make([]*chatv1.Member, 0, len(view.Members))
	for _, m := range view.Members {
		members = append(members, toProtoMember(m))
	}

	return connect.NewResponse(&chatv1.GetRoomResponse{
		Room:    toProtoRoom(view.Room),
		Members: members,
	}), nil
}

func (h *Handler) JoinRoom(
	ctx context.Context,
	req *connect.Request[chatv1.JoinRoomRequest],
) (*connect.Response[chatv1.JoinRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	room, joined, err := h.svc.JoinRoom(ctx, service.JoinRoomInput{
		RoomID:      roomID,
		UserID:      claims.UserID,
		DisplayName: h.names.DisplayName(ctx, identity.BearerFromHeader(req.Header())),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&chatv1.JoinRoomResponse{
		Room:   toProtoRoom(room),
		Joined: joined,
	}), nil
}

func (h *Handler) LeaveRoom(
	ctx context.Context,
	req *connect.Request[chatv1.LeaveRoomRequest],
) (*connect.Response[chatv1.LeaveRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	if err := h.svc.LeaveRoom(ctx, roomID, claims.UserID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&chatv1.LeaveRoomResponse{}), nil
}

func (h *Handler) ListMessages(
	ctx context.Context,
	req *connect.Request[chatv1.ListMessagesRequest],
) (*connect.Response[chatv1.ListMessagesResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	history, err := h.svc.ListMessages(ctx, roomID, claims.UserID, domain.Page{
		Limit:     int(req.Msg.GetLimit()),
		BeforeSeq: req.Msg.GetBeforeSeq(),
		AfterSeq:  req.Msg.GetAfterSeq(),
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	messages := make([]*chatv1.Message, 0, len(history.Messages))
	for _, m := range history.Messages {
		messages = append(messages, toProtoMessage(m))
	}

	return connect.NewResponse(&chatv1.ListMessagesResponse{
		Messages: messages,
		HasMore:  history.HasMore,
	}), nil
}

func (h *Handler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}

func parseID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &domain.ValidationError{Field: field, Reason: "is not a valid id"}
	}
	return id, nil
}

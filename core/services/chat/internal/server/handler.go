package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/identity"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

type Names interface {
	DisplayName(ctx context.Context, accessToken string) string
}

type People interface {
	Profiles(ctx context.Context, accessToken string, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

type Handler struct {
	svc      *service.Service
	verifier *authn.Verifier
	names    Names
	people   People
	bus      pubsub.Bus
	signals  pubsub.SignalBus
	users    pubsub.UserSignalBus
	log      *slog.Logger
}

var _ chatv1connect.ChatServiceHandler = (*Handler)(nil)

type Config struct {
	Service  *service.Service
	Verifier *authn.Verifier
	Names    Names
	People   People
	Bus      pubsub.Bus
	Signals  pubsub.SignalBus
	Users    pubsub.UserSignalBus
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
	case cfg.People == nil:
		return nil, errors.New("server: a people directory is required")
	case cfg.Bus == nil:
		return nil, errors.New("server: a message bus is required")
	case cfg.Signals == nil:
		return nil, errors.New("server: a room signal bus is required")
	case cfg.Users == nil:
		return nil, errors.New("server: a user signal bus is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}

	return &Handler{
		svc:      cfg.Service,
		verifier: cfg.Verifier,
		names:    cfg.Names,
		people:   cfg.People,
		bus:      cfg.Bus,
		signals:  cfg.Signals,
		users:    cfg.Users,
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

	return connect.NewResponse(&chatv1.CreateRoomResponse{Room: toProtoRoom(room, claims.UserID)}), nil
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
		out = append(out, toProtoRoom(room, claims.UserID))
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
		Room:    toProtoRoom(view.Room, claims.UserID),
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
		Room:   toProtoRoom(room, claims.UserID),
		Joined: joined,
	}), nil
}

func (h *Handler) HideRoom(
	ctx context.Context,
	req *connect.Request[chatv1.HideRoomRequest],
) (*connect.Response[chatv1.HideRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	if err := h.svc.HideRoom(ctx, roomID, claims.UserID); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	return connect.NewResponse(&chatv1.HideRoomResponse{}), nil
}

func (h *Handler) MarkRead(
	ctx context.Context,
	req *connect.Request[chatv1.MarkReadRequest],
) (*connect.Response[chatv1.MarkReadResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	seq := req.Msg.GetSeq()
	if err := h.svc.MarkRead(ctx, roomID, claims.UserID, seq); err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	if err := h.signals.PublishSignal(ctx, pubsub.Signal{
		Kind:   pubsub.SignalRead,
		RoomID: roomID,
		UserID: claims.UserID,
		Seq:    seq,
	}); err != nil {
		h.log.ErrorContext(ctx, "could not publish a read receipt", "room_id", roomID, "error", err)
	}

	return connect.NewResponse(&chatv1.MarkReadResponse{}), nil
}

func (h *Handler) GetDirectRoom(
	ctx context.Context,
	req *connect.Request[chatv1.GetDirectRoomRequest],
) (*connect.Response[chatv1.GetDirectRoomResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	peerID, err := parseID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	room, err := h.svc.GetDirectRoom(ctx, claims.UserID, peerID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	resp := &chatv1.GetDirectRoomResponse{}
	if room != nil {
		resp.Room = toProtoRoom(*room, claims.UserID)
	}

	return connect.NewResponse(resp), nil
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

	change, err := h.svc.LeaveRoom(ctx, roomID, claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	h.announce(ctx, change)

	return connect.NewResponse(&chatv1.LeaveRoomResponse{}), nil
}

func (h *Handler) CreateGroup(
	ctx context.Context,
	req *connect.Request[chatv1.CreateGroupRequest],
) (*connect.Response[chatv1.CreateGroupResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	token := identity.BearerFromHeader(req.Header())
	members, names, err := h.groupMembers(ctx, token, "member_user_ids", req.Msg.GetMemberUserIds(), claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	change, err := h.svc.CreateGroup(ctx, service.CreateGroupInput{
		Name:    req.Msg.GetName(),
		Owner:   service.GroupMember{UserID: claims.UserID, DisplayName: names[claims.UserID]},
		Members: members,
	})
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	h.announce(ctx, change)

	return connect.NewResponse(&chatv1.CreateGroupResponse{Room: toProtoRoom(change.Room, claims.UserID)}), nil
}

func (h *Handler) AddGroupMembers(
	ctx context.Context,
	req *connect.Request[chatv1.AddGroupMembersRequest],
) (*connect.Response[chatv1.AddGroupMembersResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	people, _, err := h.groupMembers(ctx, identity.BearerFromHeader(req.Header()),
		"user_ids", req.Msg.GetUserIds(), claims.UserID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	change, err := h.svc.AddGroupMembers(ctx, roomID, claims.UserID, people)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	h.announce(ctx, change)

	names := make(map[uuid.UUID]string, len(people))
	for _, p := range people {
		names[p.UserID] = p.DisplayName
	}
	added := make([]*chatv1.Member, 0, len(change.Added))
	for _, id := range change.Added {
		added = append(added, toProtoMember(domain.Member{
			UserID:      id,
			DisplayName: domain.ValidateDisplayName(names[id]),
			Role:        domain.MemberRoleMember,
		}))
	}

	return connect.NewResponse(&chatv1.AddGroupMembersResponse{Added: added}), nil
}

func (h *Handler) RemoveGroupMember(
	ctx context.Context,
	req *connect.Request[chatv1.RemoveGroupMemberRequest],
) (*connect.Response[chatv1.RemoveGroupMemberResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	targetID, err := parseID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	change, err := h.svc.RemoveGroupMember(ctx, roomID, claims.UserID, targetID)
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	h.announce(ctx, change)

	return connect.NewResponse(&chatv1.RemoveGroupMemberResponse{}), nil
}

func (h *Handler) RenameGroup(
	ctx context.Context,
	req *connect.Request[chatv1.RenameGroupRequest],
) (*connect.Response[chatv1.RenameGroupResponse], error) {
	claims, err := h.authenticate(req.Header())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}

	change, err := h.svc.RenameGroup(ctx, roomID, claims.UserID, req.Msg.GetName())
	if err != nil {
		return nil, translateError(ctx, h.log, err)
	}
	h.announce(ctx, change)

	return connect.NewResponse(&chatv1.RenameGroupResponse{Room: toProtoRoom(change.Room, claims.UserID)}), nil
}

func (h *Handler) groupMembers(
	ctx context.Context,
	token, field string,
	raw []string,
	callerID uuid.UUID,
) ([]service.GroupMember, map[uuid.UUID]string, error) {
	ids, err := domain.ValidateUserIDs(field, raw, callerID)
	if err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, &domain.ValidationError{Field: field, Reason: "needs at least one person"}
	}

	names, err := h.people.Profiles(ctx, token, append(slices.Clip(ids), callerID))
	if err != nil {
		return nil, nil, err
	}

	members := make([]service.GroupMember, len(ids))
	for i, id := range ids {
		members[i] = service.GroupMember{UserID: id, DisplayName: names[id]}
	}
	return members, names, nil
}

func (h *Handler) announce(ctx context.Context, change service.GroupChange) {
	ctx = context.WithoutCancel(ctx)

	for _, userID := range change.Removed {
		h.signalUser(ctx, userID, pubsub.SignalRoomRemoved, change.Room.ID)
	}
	for _, event := range change.Events {
		if err := h.bus.Publish(ctx, event); err != nil {
			h.log.ErrorContext(ctx, "could not fan a group event out",
				"room_id", event.RoomID, "message_id", event.ID, "error", err)
		}
		for _, userID := range event.Revealed {
			h.signalUser(ctx, userID, pubsub.SignalRoomAdded, event.RoomID)
		}
	}
	for _, userID := range change.Added {
		h.signalUser(ctx, userID, pubsub.SignalRoomAdded, change.Room.ID)
	}
}

func (h *Handler) signalUser(ctx context.Context, userID uuid.UUID, kind pubsub.SignalKind, roomID uuid.UUID) {
	if err := h.users.PublishUserSignal(ctx, userID, pubsub.Signal{Kind: kind, RoomID: roomID, UserID: userID}); err != nil {
		h.log.WarnContext(ctx, "could not tell a user about a room change",
			"room_id", roomID, "user_id", userID, "kind", kind, "error", err)
	}
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
		Kinds:     fromProtoMessageKinds(req.Msg.GetKinds()),
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

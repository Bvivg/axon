package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1/chatv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authz"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const adminService = "chat"

var AdminRules = authz.Rules{
	chatv1connect.ChatAdminServiceListChatRoomsProcedure:        authz.ChatRead,
	chatv1connect.ChatAdminServiceGetChatRoomProcedure:          authz.ChatRead,
	chatv1connect.ChatAdminServiceListChatRoomMessagesProcedure: authz.ChatModerate,
	chatv1connect.ChatAdminServiceDeleteChatMessageProcedure:    authz.ChatModerate,
	chatv1connect.ChatAdminServiceRemoveChatMemberProcedure:     authz.ChatModerate,
	chatv1connect.ChatAdminServiceListChatUploadsProcedure:      authz.ChatRead,
	callsv1connect.CallsAdminServiceListAllCallsProcedure:       authz.ChatRead,
	callsv1connect.CallsAdminServiceTerminateCallProcedure:      authz.ChatModerate,
}

type ChatAdmin struct {
	h *Handler
}

var _ chatv1connect.ChatAdminServiceHandler = (*ChatAdmin)(nil)

func NewChatAdmin(h *Handler) *ChatAdmin {
	return &ChatAdmin{h: h}
}

func (a *ChatAdmin) ListChatRooms(ctx context.Context, req *connect.Request[chatv1.ListChatRoomsRequest]) (*connect.Response[chatv1.ListChatRoomsResponse], error) {
	filter := domain.AdminRoomFilter{
		Query:  req.Msg.GetQuery(),
		Cursor: req.Msg.GetCursor(),
		Limit:  int(req.Msg.GetPageSize()),
	}
	switch req.Msg.GetKind() {
	case chatv1.RoomKind_ROOM_KIND_OPEN:
		filter.Kind = domain.RoomKindOpen
	case chatv1.RoomKind_ROOM_KIND_DIRECT:
		filter.Kind = domain.RoomKindDirect
	case chatv1.RoomKind_ROOM_KIND_GROUP:
		filter.Kind = domain.RoomKindGroup
	}
	if raw := req.Msg.GetMemberId(); raw != "" {
		member, err := parseID("member_id", raw)
		if err != nil {
			return nil, translateError(ctx, a.h.log, err)
		}
		filter.Member = &member
	}
	rooms, next, total, err := a.h.svc.AdminRooms(ctx, filter)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	out := &chatv1.ListChatRoomsResponse{NextCursor: next, Total: total, Rooms: make([]*chatv1.AdminRoom, len(rooms))}
	for i, room := range rooms {
		out.Rooms[i] = toProtoAdminRoom(room)
	}
	return connect.NewResponse(out), nil
}

func (a *ChatAdmin) GetChatRoom(ctx context.Context, req *connect.Request[chatv1.GetChatRoomRequest]) (*connect.Response[chatv1.GetChatRoomResponse], error) {
	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	view, err := a.h.svc.AdminRoom(ctx, roomID)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	out := &chatv1.GetChatRoomResponse{Room: toProtoAdminRoom(view.Room), Members: make([]*chatv1.Member, len(view.Members))}
	for i, member := range view.Members {
		out.Members[i] = toProtoMember(member)
	}
	return connect.NewResponse(out), nil
}

func (a *ChatAdmin) ListChatRoomMessages(ctx context.Context, req *connect.Request[chatv1.ListChatRoomMessagesRequest]) (*connect.Response[chatv1.ListChatRoomMessagesResponse], error) {
	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	action := authz.NewAction(ctx, adminService, "room_messages_viewed", "room", roomID.String(), reason)
	history, err := a.h.svc.AdminMessages(ctx, roomID, req.Msg.GetBeforeSeq(), int(req.Msg.GetPageSize()), action)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	out := &chatv1.ListChatRoomMessagesResponse{NextBeforeSeq: history.NextSeq, Messages: make([]*chatv1.Message, len(history.Messages))}
	for i, message := range history.Messages {
		out.Messages[i] = toProtoMessage(message)
	}
	return connect.NewResponse(out), nil
}

func (a *ChatAdmin) DeleteChatMessage(ctx context.Context, req *connect.Request[chatv1.DeleteChatMessageRequest]) (*connect.Response[chatv1.DeleteChatMessageResponse], error) {
	messageID, err := parseID("message_id", req.Msg.GetMessageId())
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	action := authz.NewAction(ctx, adminService, "message_deleted", "message", messageID.String(), reason)
	deleted, err := a.h.svc.ModerateDeleteMessage(ctx, messageID, action)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	if err := a.h.bus.PublishUpdate(context.WithoutCancel(ctx), deleted); err != nil {
		a.h.log.ErrorContext(ctx, "could not fan a moderated deletion out", "room_id", deleted.RoomID, "message_id", deleted.ID, "error", err)
	}
	return connect.NewResponse(&chatv1.DeleteChatMessageResponse{Message: toProtoMessage(deleted)}), nil
}

func (a *ChatAdmin) RemoveChatMember(ctx context.Context, req *connect.Request[chatv1.RemoveChatMemberRequest]) (*connect.Response[chatv1.RemoveChatMemberResponse], error) {
	roomID, err := parseID("room_id", req.Msg.GetRoomId())
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	userID, err := parseID("user_id", req.Msg.GetUserId())
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	action := authz.NewAction(ctx, adminService, "member_removed", "room", roomID.String(), reason)
	change, err := a.h.svc.ModerateRemoveMember(ctx, roomID, userID, action)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	a.h.announce(ctx, change)
	return connect.NewResponse(&chatv1.RemoveChatMemberResponse{}), nil
}

func (a *ChatAdmin) ListChatUploads(ctx context.Context, req *connect.Request[chatv1.ListChatUploadsRequest]) (*connect.Response[chatv1.ListChatUploadsResponse], error) {
	filter := domain.AdminUploadFilter{
		Kind:   domain.MessageKind(req.Msg.GetKind()),
		Cursor: req.Msg.GetCursor(),
		Limit:  int(req.Msg.GetPageSize()),
	}
	if raw := req.Msg.GetUploaderId(); raw != "" {
		uploader, err := parseID("uploader_id", raw)
		if err != nil {
			return nil, translateError(ctx, a.h.log, err)
		}
		filter.Uploader = &uploader
	}
	if raw := req.Msg.GetRoomId(); raw != "" {
		room, err := parseID("room_id", raw)
		if err != nil {
			return nil, translateError(ctx, a.h.log, err)
		}
		filter.Room = &room
	}
	uploads, next, err := a.h.svc.AdminUploads(ctx, filter)
	if err != nil {
		return nil, translateError(ctx, a.h.log, err)
	}
	out := &chatv1.ListChatUploadsResponse{NextCursor: next, Uploads: make([]*chatv1.AdminUpload, len(uploads))}
	for i, upload := range uploads {
		out.Uploads[i] = toProtoAdminUpload(upload)
	}
	return connect.NewResponse(out), nil
}

func toProtoAdminUpload(u domain.AdminUpload) *chatv1.AdminUpload {
	out := &chatv1.AdminUpload{
		Id:          u.ID.String(),
		UploaderId:  u.UploaderID.String(),
		Kind:        string(u.Kind),
		Name:        u.Name,
		ContentType: u.ContentType,
		SizeBytes:   u.SizeBytes,
		CreatedAt:   timestamppb.New(u.CreatedAt),
	}
	if u.RoomID != nil {
		out.RoomId = u.RoomID.String()
	}
	if u.MessageID != nil {
		out.MessageId = u.MessageID.String()
	}
	return out
}

func toProtoAdminRoom(r domain.AdminRoom) *chatv1.AdminRoom {
	out := &chatv1.AdminRoom{Room: toProtoRoom(r.Room, uuid.Nil), LastSeq: r.LastSeq}
	if r.LastActivity != nil {
		out.LastActivityAt = timestamppb.New(*r.LastActivity)
	}
	return out
}

type CallsAdmin struct {
	service *calls.Service
	failure func(context.Context, error) error
}

var _ callsv1connect.CallsAdminServiceHandler = (*CallsAdmin)(nil)

func NewCallsAdmin(h *CallsHandler) *CallsAdmin {
	return &CallsAdmin{service: h.service, failure: h.failure}
}

func (a *CallsAdmin) ListAllCalls(ctx context.Context, req *connect.Request[callv1.ListAllCallsRequest]) (*connect.Response[callv1.ListAllCallsResponse], error) {
	filter := calls.AdminFilter{ActiveOnly: req.Msg.GetActiveOnly(), Cursor: req.Msg.GetCursor(), Limit: int(req.Msg.GetPageSize())}
	if raw := req.Msg.GetUserId(); raw != "" {
		user, err := uuid.Parse(raw)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id is not a valid id"))
		}
		filter.User = &user
	}
	page, err := a.service.AdminCalls(ctx, filter)
	if err != nil {
		return nil, a.failure(ctx, err)
	}
	out := &callv1.ListAllCallsResponse{NextCursor: page.Next, ActiveTotal: page.ActiveTotal, Calls: make([]*callv1.Call, len(page.Calls))}
	for i, call := range page.Calls {
		out.Calls[i] = calls.ToProto(call)
	}
	return connect.NewResponse(out), nil
}

func (a *CallsAdmin) TerminateCall(ctx context.Context, req *connect.Request[callv1.TerminateCallRequest]) (*connect.Response[callv1.TerminateCallResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCallId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("call_id is not a valid id"))
	}
	reason, err := authz.Reason(req.Msg.GetReason())
	if err != nil {
		return nil, err
	}
	call, err := a.service.Terminate(ctx, id, authz.NewAction(ctx, adminService, "call_terminated", "call", id.String(), reason))
	switch {
	case errors.Is(err, calls.ErrConflict):
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the call has already ended"))
	case err != nil:
		return nil, a.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.TerminateCallResponse{Call: calls.ToProto(call)}), nil
}

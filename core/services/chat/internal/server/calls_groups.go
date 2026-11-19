package server

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

func (h *CallsHandler) GetCallCapabilities(_ context.Context, req *connect.Request[callv1.GetCallCapabilitiesRequest]) (*connect.Response[callv1.GetCallCapabilitiesResponse], error) {
	if _, err := h.actor(req.Header()); err != nil {
		return nil, err
	}
	migrationEnabled := h.service.MigrationEnabled()
	return connect.NewResponse(&callv1.GetCallCapabilitiesResponse{SfuEnabled: h.service.SFUEnabled(), MaxParticipants: calls.MaxParticipants, MigrationEnabled: &migrationEnabled}), nil
}
func (h *CallsHandler) StartGroupCall(ctx context.Context, req *connect.Request[callv1.StartGroupCallRequest]) (*connect.Response[callv1.StartGroupCallResponse], error) {
	if len(req.Msg.ParticipantUserIds) > calls.MaxParticipants-1 {
		return nil, h.failure(ctx, calls.ErrInvalid)
	}
	mode := ""
	if req.Msg.Mode == callv1.CallMode_CALL_MODE_AUDIO {
		mode = "audio"
	}
	if req.Msg.Mode == callv1.CallMode_CALL_MODE_VIDEO {
		mode = "video"
	}
	users := make([]uuid.UUID, 0, len(req.Msg.ParticipantUserIds))
	for _, raw := range req.Msg.ParticipantUserIds {
		id := callID(raw)
		if id == uuid.Nil {
			return nil, h.failure(ctx, calls.ErrInvalid)
		}
		users = append(users, id)
	}
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "start_group", Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), RoomID: callID(req.Msg.RoomId), Mode: mode, Users: users})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.StartGroupCallResponse{Call: call}), nil
}
func (h *CallsHandler) StartGroupCallFromCall(ctx context.Context, req *connect.Request[callv1.StartGroupCallFromCallRequest]) (*connect.Response[callv1.StartGroupCallFromCallResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	call, err := h.service.FromCall(ctx, actor, calls.Command{Action: "replace", Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), ID: callID(req.Msg.CallId), TargetID: callID(req.Msg.UserId)})
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.StartGroupCallFromCallResponse{Call: calls.ToProtoFor(call, actor.UserID)}), nil
}
func (h *CallsHandler) InviteCallParticipant(ctx context.Context, req *connect.Request[callv1.InviteCallParticipantRequest]) (*connect.Response[callv1.InviteCallParticipantResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "invite", Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), ID: callID(req.Msg.CallId), TargetID: callID(req.Msg.UserId)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.InviteCallParticipantResponse{Call: call}), nil
}
func (h *CallsHandler) LeaveCall(ctx context.Context, req *connect.Request[callv1.LeaveCallRequest]) (*connect.Response[callv1.LeaveCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "leave", Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), ID: callID(req.Msg.CallId)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.LeaveCallResponse{Call: call}), nil
}
func (h *CallsHandler) RemoveCallParticipant(ctx context.Context, req *connect.Request[callv1.RemoveCallParticipantRequest]) (*connect.Response[callv1.RemoveCallParticipantResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "remove", Endpoint: calls.RefFromProto(req.Msg.Endpoint), RequestID: callID(req.Msg.RequestId), ID: callID(req.Msg.CallId), TargetID: callID(req.Msg.UserId)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.RemoveCallParticipantResponse{Call: call}), nil
}
func (h *CallsHandler) GetSfuCredentials(ctx context.Context, req *connect.Request[callv1.GetSfuCredentialsRequest]) (*connect.Response[callv1.GetSfuCredentialsResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	url, token, servers, expires, err := h.service.SfuCredentials(ctx, actor, calls.RefFromProto(req.Msg.Endpoint), callID(req.Msg.CallId), req.Msg.Generation)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.GetSfuCredentialsResponse{Url: url, Token: token, IceServers: servers, ExpiresAt: calls.LeaseProto(expires)}), nil
}

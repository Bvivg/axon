package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type CallsHandler struct {
	service  *calls.Service
	verifier *authn.Verifier
	log      *slog.Logger
}

var _ callsv1connect.CallsServiceHandler = (*CallsHandler)(nil)

func NewCalls(service *calls.Service, verifier *authn.Verifier, log *slog.Logger) *CallsHandler {
	return &CallsHandler{service: service, verifier: verifier, log: log}
}
func (h *CallsHandler) actor(headers http.Header) (calls.Actor, error) {
	token, err := authn.BearerToken(headers)
	if err != nil {
		return calls.Actor{}, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	claims, err := h.verifier.Verify(token)
	if err != nil {
		return calls.Actor{}, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}
	return calls.Actor{UserID: claims.UserID, FamilyID: claims.FamilyID, Token: token}, nil
}
func (h *CallsHandler) failure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	mappings := []struct {
		err  error
		code connect.Code
	}{{calls.ErrInvalid, connect.CodeInvalidArgument}, {calls.ErrForbidden, connect.CodePermissionDenied}, {calls.ErrNotFound, connect.CodeNotFound}, {calls.ErrBusy, connect.CodeFailedPrecondition}, {calls.ErrConflict, connect.CodeAlreadyExists}, {calls.ErrStale, connect.CodeFailedPrecondition}, {calls.ErrUnavailable, connect.CodeUnavailable}}
	for _, mapping := range mappings {
		if errors.Is(err, mapping.err) {
			return connect.NewError(mapping.code, mapping.err)
		}
	}
	if code := connect.CodeOf(err); code == connect.CodeUnavailable || code == connect.CodeUnauthenticated || code == connect.CodeDeadlineExceeded {
		return connect.NewError(code, errors.New("calls dependency unavailable"))
	}
	h.log.ErrorContext(ctx, "calls request failed", "error", err)
	return connect.NewError(connect.CodeInternal, errors.New("calls request failed"))
}
func (h *CallsHandler) control(ctx context.Context, headers http.Header, cmd calls.Command) (*callv1.Call, error) {
	actor, err := h.actor(headers)
	if err != nil {
		return nil, err
	}
	call, err := h.service.Execute(ctx, actor, cmd)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return calls.ToProtoFor(call, actor.UserID), nil
}
func callID(raw string) uuid.UUID { id, _ := uuid.Parse(raw); return id }
func (h *CallsHandler) RegisterEndpoint(ctx context.Context, req *connect.Request[callv1.RegisterEndpointRequest]) (*connect.Response[callv1.RegisterEndpointResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	var resume *calls.EndpointRef
	if req.Msg.Resume != nil {
		ref := calls.RefFromProto(req.Msg.Resume)
		resume = &ref
	}
	endpoint, ref, err := h.service.RegisterMigrationEndpoint(ctx, actor, resume, req.Msg.GetSupportsSfu(), req.Msg.GetSupportsMigration())
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.RegisterEndpointResponse{Endpoint: &callv1.EndpointRef{Id: ref.ID.String(), Epoch: ref.Epoch, Proof: ref.Proof}, LeaseExpiresAt: calls.LeaseProto(endpoint.LeaseExpiresAt)}), nil
}
func callMode(mode callv1.CallMode) string {
	switch mode {
	case callv1.CallMode_CALL_MODE_AUDIO:
		return "audio"
	case callv1.CallMode_CALL_MODE_VIDEO:
		return "video"
	default:
		return ""
	}
}
func (h *CallsHandler) StartCall(ctx context.Context, req *connect.Request[callv1.StartCallRequest]) (*connect.Response[callv1.StartCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "start", RoomID: callID(req.Msg.RoomId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), Mode: callMode(req.Msg.Mode)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.StartCallResponse{Call: call}), nil
}
func (h *CallsHandler) StartDirectCall(ctx context.Context, req *connect.Request[callv1.StartDirectCallRequest]) (*connect.Response[callv1.StartDirectCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "start_direct", TargetID: callID(req.Msg.PeerUserId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), Mode: callMode(req.Msg.Mode)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.StartDirectCallResponse{Call: call}), nil
}
func (h *CallsHandler) AcceptCall(ctx context.Context, req *connect.Request[callv1.AcceptCallRequest]) (*connect.Response[callv1.AcceptCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "accept", ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.AcceptCallResponse{Call: call}), nil
}
func (h *CallsHandler) DeclineCall(ctx context.Context, req *connect.Request[callv1.DeclineCallRequest]) (*connect.Response[callv1.DeclineCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "decline", ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.DeclineCallResponse{Call: call}), nil
}
func (h *CallsHandler) CancelCall(ctx context.Context, req *connect.Request[callv1.CancelCallRequest]) (*connect.Response[callv1.CancelCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "cancel", ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.CancelCallResponse{Call: call}), nil
}
func (h *CallsHandler) EndCall(ctx context.Context, req *connect.Request[callv1.EndCallRequest]) (*connect.Response[callv1.EndCallResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "end", ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint)})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.EndCallResponse{Call: call}), nil
}
func (h *CallsHandler) ReportConnection(ctx context.Context, req *connect.Request[callv1.ReportConnectionRequest]) (*connect.Response[callv1.ReportConnectionResponse], error) {
	action := "ready"
	if req.Msg.GetReconnecting() {
		if req.Msg.Ready {
			return nil, h.failure(ctx, calls.ErrInvalid)
		}
		action = "reconnecting"
	}
	call, err := h.control(ctx, req.Header(), calls.Command{Action: action, ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), Generation: req.Msg.Generation, Ready: req.Msg.Ready, MediaVersion: req.Msg.GetMediaVersion()})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.ReportConnectionResponse{Call: call}), nil
}
func (h *CallsHandler) AcknowledgeCleanup(ctx context.Context, req *connect.Request[callv1.AcknowledgeCleanupRequest]) (*connect.Response[callv1.AcknowledgeCleanupResponse], error) {
	call, err := h.control(ctx, req.Header(), calls.Command{Action: "cleanup", ID: callID(req.Msg.CallId), RequestID: callID(req.Msg.RequestId), Endpoint: calls.RefFromProto(req.Msg.Endpoint), Generation: req.Msg.Generation, MediaVersion: req.Msg.GetMediaVersion()})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&callv1.AcknowledgeCleanupResponse{Call: call}), nil
}

func (h *CallsHandler) GetCall(ctx context.Context, req *connect.Request[callv1.GetCallRequest]) (*connect.Response[callv1.GetCallResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	call, err := h.service.Get(ctx, actor, callID(req.Msg.CallId))
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.GetCallResponse{Call: calls.ToProtoFor(call, actor.UserID)}), nil
}
func (h *CallsHandler) GetActiveCall(ctx context.Context, req *connect.Request[callv1.GetActiveCallRequest]) (*connect.Response[callv1.GetActiveCallResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	call, err := h.service.Active(ctx, actor)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	result := &callv1.GetActiveCallResponse{}
	if call != nil {
		result.Call = calls.ToProtoFor(*call, actor.UserID)
	}
	return connect.NewResponse(result), nil
}
func (h *CallsHandler) ListCalls(ctx context.Context, req *connect.Request[callv1.ListCallsRequest]) (*connect.Response[callv1.ListCallsResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	before := uuid.Nil
	if req.Msg.BeforeId != nil {
		before = callID(*req.Msg.BeforeId)
		if before == uuid.Nil {
			return nil, h.failure(ctx, calls.ErrInvalid)
		}
	}
	limit := int(req.Msg.Limit)
	if limit < 1 {
		limit = 25
	}
	if limit > 50 {
		limit = 50
	}
	history, err := h.service.History(ctx, actor, before, limit+1)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	result := &callv1.ListCallsResponse{}
	if len(history) > limit {
		history = history[:limit]
		id := history[len(history)-1].ID.String()
		result.NextBeforeId = &id
	}
	for _, call := range history {
		result.Calls = append(result.Calls, calls.ToProtoFor(call, actor.UserID))
	}
	return connect.NewResponse(result), nil
}
func (h *CallsHandler) GetMediaCredentials(ctx context.Context, req *connect.Request[callv1.GetMediaCredentialsRequest]) (*connect.Response[callv1.GetMediaCredentialsResponse], error) {
	actor, err := h.actor(req.Header())
	if err != nil {
		return nil, err
	}
	servers, expires, err := h.service.Credentials(ctx, actor, calls.RefFromProto(req.Msg.Endpoint), callID(req.Msg.CallId), req.Msg.Generation)
	if err != nil {
		return nil, h.failure(ctx, err)
	}
	return connect.NewResponse(&callv1.GetMediaCredentialsResponse{IceServers: servers, ExpiresAt: calls.LeaseProto(expires)}), nil
}

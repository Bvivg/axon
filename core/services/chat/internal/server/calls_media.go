package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
)

type MediaAdmissionHandler struct {
	Admission  *calls.MediaAdmission
	Reconciler *calls.MediaReconciler
}

func (h *MediaAdmissionHandler) CheckMediaAdmission(ctx context.Context, req *connect.Request[callv1.CheckMediaAdmissionRequest]) (*connect.Response[callv1.CheckMediaAdmissionResponse], error) {
	if !internalrpc.Authorized(req.Header(), h.Admission.InternalKey) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("internal authentication required"))
	}
	binding, err := h.Admission.Check(ctx, req.Msg.Token)
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("media admission denied"))
	}
	return connect.NewResponse(&callv1.CheckMediaAdmissionResponse{Room: binding.Room, Identity: binding.Identity}), nil
}

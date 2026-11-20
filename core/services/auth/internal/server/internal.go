package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
)

type SessionControl struct {
	Handler *Handler
	Key     string
}

func (h *SessionControl) CheckFamily(ctx context.Context, req *connect.Request[authv1.CheckFamilyRequest]) (*connect.Response[authv1.CheckFamilyResponse], error) {
	if !internalrpc.Authorized(req.Header(), h.Key) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("internal authentication required"))
	}
	user, err := uuid.Parse(req.Msg.UserId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid user"))
	}
	family, err := uuid.Parse(req.Msg.FamilyId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid family"))
	}
	active, err := h.Handler.svc.SessionFamilyActive(ctx, user, family)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("session lookup unavailable"))
	}
	return connect.NewResponse(&authv1.CheckFamilyResponse{Active: active}), nil
}

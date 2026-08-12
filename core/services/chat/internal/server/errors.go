package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

func translateError(ctx context.Context, log *slog.Logger, err error) error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	if v, ok := domain.AsValidationError(err); ok {
		return connect.NewError(connect.CodeInvalidArgument, errors.New(v.Field+" "+v.Reason))
	}

	switch {
	case errors.Is(err, authn.ErrNoToken):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))

	case errors.Is(err, authn.ErrInvalidToken),
		errors.Is(err, authn.ErrExpired),
		errors.Is(err, authn.ErrUnknownKeyID):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))

	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrRoomNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("room not found"))

	case errors.Is(err, domain.ErrMessageNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("message not found"))
	}

	log.ErrorContext(ctx, "unhandled error", "error", err)

	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

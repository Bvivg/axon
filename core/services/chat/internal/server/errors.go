package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

// translateError maps a domain failure onto a Connect code.
//
// This is the only place the mapping happens. Doing it per handler is how a
// service ends up returning Internal for something the client could have fixed,
// or Unauthenticated for a genuine outage.
//
// Anything unrecognised becomes Internal with a fixed message. An unexpected
// error is by definition one nobody reasoned about, and its text can carry a
// query, a DSN or a row — none of which belongs on the wire.
func translateError(ctx context.Context, log *slog.Logger, err error) error {
	if err == nil {
		return nil
	}

	// Already a Connect error: a nested handler decided the code, keep it.
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr
	}

	// Field-level validation names the offending field, because a client can
	// only fix what it is told about.
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

	// A room the caller is not in and a room that does not exist get the same
	// answer, which is the whole point of the two domain errors collapsing here:
	// distinguishing them would turn any room id into a probe for whether that
	// room exists.
	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrRoomNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("room not found"))

	case errors.Is(err, domain.ErrMessageNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("message not found"))
	}

	// Nothing recognised it, so it is a failure rather than a refusal. Logged
	// here with its detail, returned without.
	log.ErrorContext(ctx, "unhandled error", "error", err)

	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

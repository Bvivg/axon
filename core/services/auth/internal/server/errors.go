package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
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
	// Everything a failed sign-in can be collapses to one code and one message.
	// Distinguishing them here would undo the care taken in the service layer to
	// keep login from being a user-enumeration oracle.
	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrNoPassword):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid email or password"))

	case errors.Is(err, domain.ErrEmailTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("email already registered"))

	case errors.Is(err, domain.ErrUserNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("user not found"))

	// Reuse and plain invalidity look identical to the client on purpose: the
	// answer is the same, sign in again. The difference is recorded in the logs,
	// where it belongs, not handed to whoever presented the token.
	case errors.Is(err, domain.ErrRefreshTokenInvalid),
		errors.Is(err, domain.ErrRefreshTokenReused):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token is not valid"))

	case errors.Is(err, authn.ErrNoToken):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("no access token"))

	// Expiry is told apart from every other token failure because the client
	// acts on it differently: refresh, rather than sign in again.
	case errors.Is(err, authn.ErrExpired):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token expired"))

	case errors.Is(err, authn.ErrInvalidToken), errors.Is(err, authn.ErrUnknownKeyID):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))

	case errors.Is(err, domain.ErrProviderUnsupported):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported oauth provider"))

	case errors.Is(err, domain.ErrOauthStateInvalid):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("oauth state is not valid"))

	// The client cannot fix these by retrying, and the person can: verify the
	// address at the provider, or use a different one. Saying which is which is
	// safe — the caller already controls the provider account involved.
	case errors.Is(err, domain.ErrOauthEmailUnverified):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the provider has not verified this email address"))

	case errors.Is(err, domain.ErrOauthProfileIncomplete):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the provider did not return an email address"))

	// Neither a retry nor a server fault: this provider account belongs to a
	// different user here. Saying so is safe — whoever is asking controls the
	// provider account in question — and it is the only way they learn that the
	// way in is the other account, not this one.
	case errors.Is(err, domain.ErrOauthIdentityClaimed):
		return connect.NewError(connect.CodeAlreadyExists,
			errors.New("this provider account is already linked to another user"))

	case errors.Is(err, domain.ErrOauthReturnToNotAllowed):
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("return_to is not an allowed destination"))

	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, errors.New("request canceled"))

	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("request timed out"))
	}

	// The real error goes to the logs, where it is useful; the client gets
	// nothing it could learn from.
	log.ErrorContext(ctx, "unhandled error in auth handler", "error", err)
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

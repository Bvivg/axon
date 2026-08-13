package server

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
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

	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrNoPassword):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid email or password"))

	case errors.Is(err, domain.ErrEmailTaken):
		return connect.NewError(connect.CodeAlreadyExists, errors.New("email already registered"))

	case errors.Is(err, domain.ErrUserNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("user not found"))

	case errors.Is(err, domain.ErrRefreshTokenInvalid),
		errors.Is(err, domain.ErrRefreshTokenReused):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token is not valid"))

	case errors.Is(err, authn.ErrNoToken):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("no access token"))

	case errors.Is(err, authn.ErrExpired):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token expired"))

	case errors.Is(err, authn.ErrInvalidToken), errors.Is(err, authn.ErrUnknownKeyID):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))

	case errors.Is(err, domain.ErrProviderUnsupported):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("unsupported oauth provider"))

	case errors.Is(err, domain.ErrOauthStateInvalid):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("oauth state is not valid"))

	case errors.Is(err, domain.ErrOauthEmailUnverified):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the provider has not verified this email address"))

	case errors.Is(err, domain.ErrOauthProfileIncomplete):
		return connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the provider did not return an email address"))

	case errors.Is(err, domain.ErrOauthIdentityClaimed):
		return connect.NewError(connect.CodeAlreadyExists,
			errors.New("this provider account is already linked to another user"))

	case errors.Is(err, domain.ErrOauthReturnToNotAllowed):
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("return_to is not an allowed destination"))

	case errors.Is(err, domain.ErrSessionNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("session not found"))

	case errors.Is(err, context.Canceled):
		return connect.NewError(connect.CodeCanceled, errors.New("request canceled"))

	case errors.Is(err, context.DeadlineExceeded):
		return connect.NewError(connect.CodeDeadlineExceeded, errors.New("request timed out"))
	}

	log.ErrorContext(ctx, "unhandled error in auth handler", "error", err)
	return connect.NewError(connect.CodeInternal, errors.New("internal error"))
}

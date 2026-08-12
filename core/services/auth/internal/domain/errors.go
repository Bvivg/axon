package domain

import "errors"

var (
	ErrUserNotFound = errors.New("auth: user not found")

	ErrEmailTaken = errors.New("auth: email already registered")

	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	ErrNoPassword = errors.New("auth: account has no password set")

	ErrRefreshTokenInvalid = errors.New("auth: refresh token invalid")

	ErrRefreshTokenReused = errors.New("auth: refresh token reused")

	ErrProviderUnsupported = errors.New("auth: oauth provider unsupported")

	ErrOauthStateInvalid = errors.New("auth: oauth state invalid")

	ErrOauthEmailUnverified = errors.New("auth: oauth provider did not verify the email address")

	ErrOauthProfileIncomplete = errors.New("auth: oauth provider returned an incomplete profile")

	ErrOauthIdentityClaimed = errors.New("auth: this provider account is already linked to another user")

	ErrOauthReturnToNotAllowed = errors.New("auth: return_to is not an allowed destination")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "auth: " + e.Field + " " + e.Reason
}

func newValidationError(field, reason string) *ValidationError {
	return &ValidationError{Field: field, Reason: reason}
}

func AsValidationError(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

package domain

import "errors"

// Domain failures are sentinel errors so callers can branch with errors.Is
// rather than matching on message text. The server layer maps each one to a
// connect.Code exactly once, in a single place.
var (
	// ErrUserNotFound is returned when no account matches the lookup.
	ErrUserNotFound = errors.New("auth: user not found")

	// ErrEmailTaken is returned when registration hits the unique constraint on
	// email.
	ErrEmailTaken = errors.New("auth: email already registered")

	// ErrInvalidCredentials covers both a wrong password and an unknown email.
	// The two are deliberately indistinguishable: telling them apart turns the
	// login endpoint into an oracle for which addresses have accounts.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")

	// ErrNoPassword is returned when a password sign-in is attempted against an
	// account that only ever authenticated through a provider.
	ErrNoPassword = errors.New("auth: account has no password set")

	// ErrRefreshTokenInvalid covers an unknown, malformed or expired refresh
	// token. Like ErrInvalidCredentials, the variants are collapsed on purpose.
	ErrRefreshTokenInvalid = errors.New("auth: refresh token invalid")

	// ErrRefreshTokenReused means a token that was already exchanged came back.
	// The whole family is revoked before this is returned; it is separate from
	// ErrRefreshTokenInvalid because it must be logged as a security event, not
	// as a routine expiry.
	ErrRefreshTokenReused = errors.New("auth: refresh token reused")

	// ErrProviderUnsupported is returned for a provider that is not configured,
	// including the fake provider outside development and test.
	ErrProviderUnsupported = errors.New("auth: oauth provider unsupported")

	// ErrOauthStateInvalid covers a missing, expired or already-consumed state
	// value on the OAuth callback.
	ErrOauthStateInvalid = errors.New("auth: oauth state invalid")
)

// ValidationError reports input that failed a domain invariant. It names the
// field so the transport layer can return something a client can act on.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "auth: " + e.Field + " " + e.Reason
}

// newValidationError is a shorthand for the constructor used across this package.
func newValidationError(field, reason string) *ValidationError {
	return &ValidationError{Field: field, Reason: reason}
}

// AsValidationError reports whether err is a ValidationError, and returns it.
func AsValidationError(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

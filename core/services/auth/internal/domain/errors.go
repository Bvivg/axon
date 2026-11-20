package domain

import (
	"errors"
	"time"
)

var (
	ErrUserNotFound = errors.New("auth: user not found")

	ErrEmailTaken = errors.New("auth: email already registered")

	ErrUsernameTaken = errors.New("auth: username already taken")

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

	ErrOauthEmailNotAllowed = errors.New("auth: the test provider only vouches for its reserved domain")

	ErrOauthAccountExists = errors.New("auth: an account with this email exists and cannot be linked")

	ErrLoginThrottled = errors.New("auth: too many failed sign-ins for this account")

	ErrPasswordHashingBusy = errors.New("auth: password hashing is at capacity")

	ErrSessionNotFound = errors.New("auth: session not found")

	ErrRoleNotFound = errors.New("auth: role not found")

	ErrPermissionDenied = errors.New("auth: permission denied")
)

type ThrottledError struct {
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string { return ErrLoginThrottled.Error() }

func (e *ThrottledError) Unwrap() error { return ErrLoginThrottled }

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

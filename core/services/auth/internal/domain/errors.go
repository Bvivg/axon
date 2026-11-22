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

	ErrOauthChallengeInvalid = errors.New("auth: app_challenge must be an S256 PKCE challenge")

	ErrOauthEmailNotAllowed = errors.New("auth: the test provider only vouches for its reserved domain")

	ErrOauthAccountExists = errors.New("auth: an account with this email exists and cannot be linked")

	ErrLoginThrottled = errors.New("auth: too many failed sign-ins for this account")

	ErrPasswordHashingBusy = errors.New("auth: password hashing is at capacity")

	ErrSessionNotFound = errors.New("auth: session not found")

	ErrRoleNotFound = errors.New("auth: role not found")

	ErrPermissionDenied = errors.New("auth: permission denied")

	ErrAccountSuspended = errors.New("auth: account is suspended")

	ErrSelfAction = errors.New("auth: administrators cannot apply this action to themselves")

	ErrLastAdmin = errors.New("auth: at least one active administrator must remain")

	ErrAlreadySuspended = errors.New("auth: account is already suspended")

	ErrNotSuspended = errors.New("auth: account is not suspended")

	ErrRoleExists = errors.New("auth: a role with this name already exists")

	ErrSystemRole = errors.New("auth: built-in roles cannot be changed")

	ErrRoleInUse = errors.New("auth: the role still has members")

	ErrUnknownPermission = errors.New("auth: unknown permission")

	ErrRequestReused = errors.New("auth: request_id was already used for another role")

	ErrOnlyLogin = errors.New("auth: this is the account's only way to sign in")

	ErrLinkedAccountNotFound = errors.New("auth: no linked account for this provider")
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

type VersionConflict struct {
	Current int64
}

func (e *VersionConflict) Error() string {
	return "auth: the record was changed by someone else"
}

func AsVersionConflict(err error) (*VersionConflict, bool) {
	var conflict *VersionConflict
	ok := errors.As(err, &conflict)
	return conflict, ok
}

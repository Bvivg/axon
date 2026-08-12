package domain

import "errors"

var (
	ErrRoomNotFound = errors.New("chat: room not found")

	ErrNotAMember = errors.New("chat: not a member of this room")

	ErrMessageNotFound = errors.New("chat: message not found")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "chat: " + e.Field + " " + e.Reason
}

func newValidationError(field, reason string) error {
	return &ValidationError{Field: field, Reason: reason}
}

func AsValidationError(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

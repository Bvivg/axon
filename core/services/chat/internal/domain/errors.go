package domain

import "errors"

// Domain failures are sentinel errors so callers branch with errors.Is rather
// than matching on message text. The server layer maps each one to a
// connect.Code exactly once, in a single place; the socket maps them to close
// codes and error frames in another.
var (
	// ErrRoomNotFound is returned when no room matches the id.
	ErrRoomNotFound = errors.New("chat: room not found")

	// ErrNotAMember is returned when the caller does not belong to the room
	// they are reading or writing.
	//
	// It is deliberately the same answer as a room that does not exist would
	// deserve, and callers must render it that way: distinguishing the two
	// turns any room id into a probe for whether that room exists.
	ErrNotAMember = errors.New("chat: not a member of this room")

	// ErrMessageNotFound is returned when a lookup by id or client id finds
	// nothing.
	ErrMessageNotFound = errors.New("chat: message not found")
)

// ValidationError reports input the domain refuses. It names the field so a
// client can point at it rather than at the form.
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

// AsValidationError reports whether err is a ValidationError, and returns it.
func AsValidationError(err error) (*ValidationError, bool) {
	var v *ValidationError
	ok := errors.As(err, &v)
	return v, ok
}

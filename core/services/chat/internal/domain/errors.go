package domain

import "errors"

var (
	ErrRoomNotFound = errors.New("chat: room not found")

	ErrNotAMember = errors.New("chat: not a member of this room")

	ErrMessageNotFound = errors.New("chat: message not found")

	ErrDirectRoomNotJoinable = errors.New("chat: direct rooms cannot be joined")

	ErrInvalidReplyTarget = errors.New("chat: reply target is not in this room")

	ErrInvalidForwardTarget = errors.New("chat: forward target is not accessible")

	ErrSystemKindNotSendable = errors.New("chat: system messages cannot be sent by a client")

	ErrCannotMessageSelf = errors.New("chat: cannot open a direct room with yourself")

	ErrUploadNotFound = errors.New("chat: upload not found")

	ErrUploadRequired = errors.New("chat: this kind of message carries an upload")

	ErrUploadKindMismatch = errors.New("chat: the upload is a different kind of message")

	ErrClientPayload = errors.New("chat: the payload comes from an upload, not from the client")

	ErrGroupNotJoinable = errors.New("chat: a group is joined only when its owner adds you")

	ErrNotAGroup = errors.New("chat: this room is not a group")

	ErrNotGroupOwner = errors.New("chat: only the group owner can do this")

	ErrGroupFull = errors.New("chat: the group has no room for more people")

	ErrUnknownUser = errors.New("chat: no such user")

	ErrNotMessageAuthor = errors.New("chat: only the author can change this message")

	ErrMessageNotEditable = errors.New("chat: this message cannot be edited")

	ErrMessageDeleted = errors.New("chat: the message was deleted")
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

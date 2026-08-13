package ws

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/coder/websocket"
)

const MaxCloseReason = 123

func TruncateReason(reason string) string {
	if len(reason) <= MaxCloseReason {
		return reason
	}

	cut := MaxCloseReason
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut]
}

func CloseCodeFor(err error) (StatusCode, string) {
	switch {
	case err == nil:
		return StatusNormalClosure, ""

	case errors.Is(err, websocket.ErrMessageTooBig):
		return StatusMessageTooBig, "message too big"

	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return StatusGoingAway, "going away"
	}

	if code := websocket.CloseStatus(err); code != -1 {
		return code, ""
	}

	return StatusInternalError, "internal error"
}

func CloseStatus(err error) StatusCode {
	return websocket.CloseStatus(err)
}

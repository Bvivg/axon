package ws

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/coder/websocket"
)

// MaxCloseReason is how many bytes of reason fit in a close frame: a control
// frame carries at most 125 bytes of payload, two of which are the status code.
//
// This matters more than it looks. The library refuses to send an over-long
// reason, so a reason built from an error message does not merely get
// truncated — the close frame is never sent at all, and the peer sees the
// connection disappear with no explanation.
const MaxCloseReason = 123

// TruncateReason cuts a reason down to what a close frame can carry, on a rune
// boundary. Cutting mid-rune would produce invalid UTF-8, which the protocol
// forbids in a close reason — the fix would break the very frame it was
// meant to repair.
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

// CloseCodeFor maps an error onto the close code and reason to hang up with.
//
// It exists so that every endpoint reports the same thing for the same failure:
// a peer that gets 1009 knows it sent too much, and one that gets 1001 knows
// the server is going away and it is worth reconnecting.
func CloseCodeFor(err error) (StatusCode, string) {
	switch {
	case err == nil:
		return StatusNormalClosure, ""

	case errors.Is(err, websocket.ErrMessageTooBig):
		return StatusMessageTooBig, "message too big"

	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return StatusGoingAway, "going away"
	}

	// The peer already picked a code by closing first; echoing it keeps the
	// two sides' logs telling the same story.
	if code := websocket.CloseStatus(err); code != -1 {
		return code, ""
	}

	// Anything left is our own failure, not the peer's, and the reason stays
	// generic on purpose: a close frame is readable by whoever is on the other
	// end, and error text is for our logs.
	return StatusInternalError, "internal error"
}

// CloseStatus reports the code a connection was closed with, or -1 when err is
// not a close at all.
//
// The counterpart to CloseCodeFor: one end decides what to say, the other has
// to be able to read it. Applications need this for their own codes above 4000
// — an expired credential and a peer that simply hung up are both "the socket
// ended", and only the code tells them apart.
func CloseStatus(err error) StatusCode {
	return websocket.CloseStatus(err)
}

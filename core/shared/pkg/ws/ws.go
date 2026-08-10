// Package ws is Axon's WebSocket transport: a thin wrapper over
// coder/websocket that adds the three things every realtime endpoint needs and
// the library deliberately leaves to its callers — keepalive pings, reconnect
// backoff with parameters spelled out, and a close path with sane status codes.
//
// It is transport, not protocol. The package moves frames and knows nothing
// about chat messages, game moves, or how either is encoded; a type in here
// that mentions a room or a session is a bug.
//
// Read must be driven continuously. That is not a stylistic preference: the
// library processes control frames — including the pongs the keepalive waits
// for — only while a read is in flight, so a connection nobody reads from will
// be torn down by its own keepalive.
package ws

import (
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/logger"
)

// StatusCode is a WebSocket close code. It is an alias, so callers can pass
// codes straight to and from the underlying library.
type StatusCode = websocket.StatusCode

// The close codes Axon actually uses. The full set lives in the library.
const (
	// StatusNormalClosure is a clean, expected shutdown of the connection.
	StatusNormalClosure = websocket.StatusNormalClosure
	// StatusGoingAway is the server shutting down or the client navigating
	// away — nobody did anything wrong.
	StatusGoingAway = websocket.StatusGoingAway
	// StatusPolicyViolation is the peer breaking a rule, e.g. sending on a
	// session it has no business in.
	StatusPolicyViolation = websocket.StatusPolicyViolation
	// StatusMessageTooBig is a frame past the read limit.
	StatusMessageTooBig = websocket.StatusMessageTooBig
	// StatusInternalError is our own failure, not the peer's.
	StatusInternalError = websocket.StatusInternalError
)

// MessageType selects the frame type written on the wire.
type MessageType = websocket.MessageType

const (
	// MessageText is the default: browsers hand a text frame to JavaScript as a
	// string, which is what a JSON protocol wants.
	MessageText = websocket.MessageText
	// MessageBinary is for encoded payloads such as protobuf.
	MessageBinary = websocket.MessageBinary
)

// Defaults for Options. They are exported constants because "explicit
// parameters" is worth little if the values are hidden in a function body.
const (
	// DefaultPingInterval is how often an idle connection is pinged. Well under
	// the 60s that proxies and load balancers commonly use to reap idle
	// connections.
	DefaultPingInterval = 30 * time.Second
	// DefaultPingTimeout is how long a pong may take before the peer counts as
	// gone.
	DefaultPingTimeout = 10 * time.Second
	// DefaultWriteTimeout bounds a single write, so one stuck peer cannot pin a
	// goroutine forever.
	DefaultWriteTimeout = 10 * time.Second
	// DefaultReadLimit caps an inbound message. Chat messages and game moves
	// are small; anything near this is either a bug or an attempt to exhaust
	// memory.
	DefaultReadLimit int64 = 32 * 1024
)

// Options are the knobs shared by both ends of a connection.
//
// The zero value is usable: every field falls back to the Default* constant
// above. Negative values switch a feature off, which is different from zero —
// zero means "whatever the default is", not "disabled".
type Options struct {
	// PingInterval is the keepalive period; negative disables keepalive.
	PingInterval time.Duration
	// PingTimeout is how long to wait for a pong before giving up on the peer.
	PingTimeout time.Duration
	// WriteTimeout bounds one write on top of the caller's context.
	WriteTimeout time.Duration
	// ReadLimit caps an inbound message in bytes; negative removes the cap.
	ReadLimit int64
	// MessageType is the frame type Write produces.
	MessageType MessageType
	// Logger receives the connection's own events. Defaults to a discarding
	// logger so a caller that does not care is not forced to pass one.
	Logger *slog.Logger
}

func (o *Options) applyDefaults() {
	if o.PingInterval == 0 {
		o.PingInterval = DefaultPingInterval
	}
	if o.PingTimeout == 0 {
		o.PingTimeout = DefaultPingTimeout
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = DefaultWriteTimeout
	}
	if o.ReadLimit == 0 {
		o.ReadLimit = DefaultReadLimit
	}
	if o.MessageType == 0 {
		o.MessageType = MessageText
	}
	if o.Logger == nil {
		o.Logger = logger.Discard()
	}
}

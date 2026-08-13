package ws

import (
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/logger"
)

type StatusCode = websocket.StatusCode

const (
	StatusNormalClosure = websocket.StatusNormalClosure

	StatusGoingAway = websocket.StatusGoingAway

	StatusPolicyViolation = websocket.StatusPolicyViolation

	StatusMessageTooBig = websocket.StatusMessageTooBig

	StatusInternalError = websocket.StatusInternalError
)

type MessageType = websocket.MessageType

const (
	MessageText = websocket.MessageText

	MessageBinary = websocket.MessageBinary
)

const (
	DefaultPingInterval = 30 * time.Second

	DefaultPingTimeout = 10 * time.Second

	DefaultWriteTimeout = 10 * time.Second

	DefaultReadLimit int64 = 32 * 1024
)

type Options struct {
	PingInterval time.Duration

	PingTimeout time.Duration

	WriteTimeout time.Duration

	ReadLimit int64

	MessageType MessageType

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

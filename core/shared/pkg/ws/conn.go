package ws

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// Conn is one WebSocket connection.
//
// All methods are safe to call concurrently except Read, which — like the
// underlying library — expects a single reader.
type Conn struct {
	conn          *websocket.Conn
	opts          Options
	logger        *slog.Logger
	correlationID string

	stopKeepalive context.CancelFunc
	closeOnce     sync.Once
	closeErr      error
}

// newConn wraps a live connection and starts its keepalive. opts must already
// have its defaults applied.
func newConn(raw *websocket.Conn, opts Options, correlationID string) *Conn {
	if opts.ReadLimit > 0 {
		raw.SetReadLimit(opts.ReadLimit)
	}

	c := &Conn{
		conn:          raw,
		opts:          opts,
		correlationID: correlationID,
		logger:        opts.Logger.With("component", "ws"),
		stopKeepalive: func() {},
	}
	c.startKeepalive()

	return c
}

// CorrelationID is the ID this connection was opened with: taken from the
// upgrade request on the server side, sent with the handshake on the client
// side. It ties a long-lived session back to the request that opened it.
func (c *Conn) CorrelationID() string { return c.correlationID }

// Subprotocol is the negotiated subprotocol, empty when there is none.
func (c *Conn) Subprotocol() string { return c.conn.Subprotocol() }

// Read returns the next message's payload.
//
// It must be called continuously for the connection to stay healthy: control
// frames, pongs included, are only processed while a read is in flight.
func (c *Conn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("ws: read: %w", err)
	}
	return data, nil
}

// Write sends one message, bounded by WriteTimeout on top of ctx.
func (c *Conn) Write(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.WriteTimeout)
	defer cancel()

	if err := c.conn.Write(ctx, c.opts.MessageType, data); err != nil {
		return fmt.Errorf("ws: write: %w", err)
	}
	return nil
}

// Close performs the closing handshake with the given code and reason, so the
// peer learns why it was disconnected instead of seeing the socket vanish.
//
// The reason is truncated to what the protocol allows; see TruncateReason.
// Closing twice is a no-op, and the second call reports the first result.
func (c *Conn) Close(code StatusCode, reason string) error {
	c.closeOnce.Do(func() {
		c.stopKeepalive()
		c.closeErr = c.conn.Close(code, TruncateReason(reason))
	})

	if c.closeErr != nil {
		return fmt.Errorf("ws: close: %w", c.closeErr)
	}
	return nil
}

// CloseNow drops the connection without the closing handshake. It is for the
// cases where the handshake cannot work anyway — an unresponsive peer, or a
// failure severe enough that waiting for a reply is pointless.
func (c *Conn) CloseNow() error {
	c.closeOnce.Do(func() {
		c.stopKeepalive()
		c.closeErr = c.conn.CloseNow()
	})

	if c.closeErr != nil {
		return fmt.Errorf("ws: close now: %w", c.closeErr)
	}
	return nil
}

// startKeepalive pings the peer on an interval and hangs up when it stops
// answering.
//
// TCP alone does not notice a peer that is powered off or behind a dropped NAT
// mapping: without pings such a connection stays open, holding a session and a
// goroutine, until the OS gives up minutes or hours later.
func (c *Conn) startKeepalive() {
	if c.opts.PingInterval <= 0 {
		return
	}

	// context.Background rather than a request context: the keepalive lives as
	// long as the connection, which outlives whatever handler set it up.
	ctx, cancel := context.WithCancel(correlation.WithID(context.Background(), c.correlationID))
	c.stopKeepalive = cancel

	go c.keepalive(ctx)
}

func (c *Conn) keepalive(ctx context.Context) {
	ticker := time.NewTicker(c.opts.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, c.opts.PingTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()

			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				// The connection is being closed on purpose; a failed ping on
				// the way out is not news.
				return
			}

			c.logger.WarnContext(ctx, "keepalive failed, dropping connection", "error", err)
			// CloseNow, not Close: a peer that did not answer a ping will not
			// answer a close frame either, and waiting on that handshake is
			// exactly the hang the keepalive exists to prevent.
			_ = c.conn.CloseNow()
			return
		}
	}
}

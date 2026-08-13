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

type Conn struct {
	conn          *websocket.Conn
	opts          Options
	logger        *slog.Logger
	correlationID string

	stopKeepalive context.CancelFunc
	closeOnce     sync.Once
	closeErr      error
}

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

func (c *Conn) CorrelationID() string { return c.correlationID }

func (c *Conn) Subprotocol() string { return c.conn.Subprotocol() }

func (c *Conn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("ws: read: %w", err)
	}
	return data, nil
}

func (c *Conn) Write(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, c.opts.WriteTimeout)
	defer cancel()

	if err := c.conn.Write(ctx, c.opts.MessageType, data); err != nil {
		return fmt.Errorf("ws: write: %w", err)
	}
	return nil
}

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

func (c *Conn) startKeepalive() {
	if c.opts.PingInterval <= 0 {
		return
	}

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

				return
			}

			c.logger.WarnContext(ctx, "keepalive failed, dropping connection", "error", err)

			_ = c.conn.CloseNow()
			return
		}
	}
}

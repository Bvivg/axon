package ws

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

const DefaultHandshakeTimeout = 10 * time.Second

type DialOptions struct {
	Options

	Subprotocols []string

	Header http.Header

	HTTPClient *http.Client

	HandshakeTimeout time.Duration
}

func Dial(ctx context.Context, url string, opts DialOptions) (*Conn, error) {
	opts.applyDefaults()
	if opts.HandshakeTimeout == 0 {
		opts.HandshakeTimeout = DefaultHandshakeTimeout
	}

	header := http.Header{}
	for key, values := range opts.Header {
		header[key] = values
	}

	id := correlation.FromContext(ctx)
	if id != "" && header.Get(correlation.Header) == "" {
		header.Set(correlation.Header, id)
	}

	handshakeCtx, cancel := context.WithTimeout(ctx, opts.HandshakeTimeout)
	defer cancel()

	raw, _, err := websocket.Dial(handshakeCtx, url, &websocket.DialOptions{ //nolint:bodyclose // owned by the library
		HTTPClient:   opts.HTTPClient,
		HTTPHeader:   header,
		Subprotocols: opts.Subprotocols,
	})
	if err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", url, err)
	}

	return newConn(raw, opts.Options, id), nil
}

func DialRetry(ctx context.Context, url string, opts DialOptions, b Backoff) (*Conn, error) {
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("ws: dial %s: %w", url, err)
	}
	opts.applyDefaults()
	log := opts.Logger.With("component", "ws", "url", url)

	for attempt := 0; ; attempt++ {
		conn, err := Dial(ctx, url, opts)
		if err == nil {
			if attempt > 0 {
				log.InfoContext(ctx, "connected after retrying", "attempts", attempt+1)
			}
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ws: dial %s: %w", url, ctx.Err())
		}

		delay := b.Delay(attempt)
		log.WarnContext(ctx, "dial failed, retrying",
			"attempt", attempt+1, "retry_in", delay.String(), "error", err)

		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("ws: dial %s: %w", url, ctx.Err())
		case <-timer.C:
		}
	}
}

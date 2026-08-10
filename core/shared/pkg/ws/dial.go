package ws

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// DefaultHandshakeTimeout bounds the upgrade request. It only covers the
// handshake: once the connection is up it has no deadline of its own, and
// liveness is the keepalive's job.
const DefaultHandshakeTimeout = 10 * time.Second

// DialOptions configure the client side of a connection.
type DialOptions struct {
	Options

	// Subprotocols to offer.
	Subprotocols []string
	// Header is sent with the upgrade request — an Authorization header, for
	// instance. The correlation ID is added automatically when ctx has one.
	Header http.Header
	// HTTPClient performs the handshake. Leave it nil unless the caller needs
	// its own transport or cookie jar.
	HTTPClient *http.Client
	// HandshakeTimeout bounds the upgrade request.
	HandshakeTimeout time.Duration
}

// Dial opens a connection. One attempt, no retries — see DialRetry.
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

	// The timeout covers the handshake only: the library derives its own
	// context for the request and hands back a connection that no longer
	// depends on it, so cancelling here does not disturb the live connection.
	handshakeCtx, cancel := context.WithTimeout(ctx, opts.HandshakeTimeout)
	defer cancel()

	// The response body belongs to the library — it documents that a caller
	// never closes it — and on success it is the connection itself.
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

// DialRetry keeps dialling until it connects or ctx ends, waiting b between
// attempts.
//
// There is no attempt limit on purpose: the caller bounds the effort with ctx,
// which is the only bound that means anything here. A client reconnecting to a
// service that is being redeployed should keep trying for as long as its own
// deadline allows, not for an arbitrary number of tries.
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

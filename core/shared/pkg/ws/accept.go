package ws

import (
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// AcceptOptions configure the server side of a connection.
type AcceptOptions struct {
	Options

	// Subprotocols the server is willing to speak, in order of preference.
	Subprotocols []string
	// OriginPatterns is the allow-list of browser origins permitted to
	// connect. An empty list means same-origin only, which is the safe default
	// and the one rules/security.md asks for: "*" belongs nowhere near this
	// field, since the browser sends cookies with the upgrade request.
	OriginPatterns []string
}

// Accept upgrades an HTTP request to a WebSocket connection.
//
// The correlation ID comes from the request — from its context when the
// correlation middleware has already run, and from the header otherwise — so
// everything the connection logs for the rest of its life stays tied to the
// request that opened it.
func Accept(w http.ResponseWriter, r *http.Request, opts AcceptOptions) (*Conn, error) {
	opts.applyDefaults()

	raw, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   opts.Subprotocols,
		OriginPatterns: opts.OriginPatterns,
	})
	if err != nil {
		return nil, fmt.Errorf("ws: accept: %w", err)
	}

	id := correlation.FromContext(r.Context())
	if id == "" {
		id = r.Header.Get(correlation.Header)
	}

	return newConn(raw, opts.Options, id), nil
}

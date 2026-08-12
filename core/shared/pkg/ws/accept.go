package ws

import (
	"fmt"
	"net/http"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

type AcceptOptions struct {
	Options

	Subprotocols []string

	OriginPatterns []string
}

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

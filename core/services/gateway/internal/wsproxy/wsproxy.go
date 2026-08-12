// Package wsproxy terminates a browser's WebSocket at the gateway and carries
// it to the service that owns the conversation.
//
// The gateway is the trust boundary (rules/security.md), and that is the whole
// argument for this file existing. Publishing chat's socket directly would mean
// a second public entrance with its own origin allow-list, its own throttling
// and its own correlation ids — reimplemented per realtime service, and there
// will be at least two.
//
// What crosses this proxy is frames, untouched. The gateway does not know what
// a chat frame means and must not learn: the protocol is the service's, and a
// proxy that parsed it would have to be changed every time the service's
// protocol was.
package wsproxy

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

// BearerPrefix carries the access token in the subprotocol list.
//
// A browser cannot set an Authorization header on an upgrade — the WebSocket
// API takes a URL and a subprotocol list and nothing else. The alternatives are
// worse: a token in the query string lands in every access log and in the
// browser's history, and a cookie would have to be readable by the page to be
// sent here at all, which is exactly what the refresh cookie is HttpOnly to
// avoid.
//
// So the token rides as a subprotocol offer, the gateway takes it off, and the
// hop to the service behind carries a real Authorization header. The service
// verifies that token itself — this proxy is not asking it to take the
// gateway's word for who is calling.
const BearerPrefix = "axon.bearer."

// Handler upgrades a browser connection and joins it to an upstream one.
type Handler struct {
	upstream string
	protocol string
	verifier *authn.Verifier
	origins  []string
	log      *slog.Logger
}

// Config configures a Handler.
type Config struct {
	// Upstream is the service's socket address, ws:// or wss://.
	Upstream string

	// Protocol is the subprotocol this endpoint speaks, which is also the one
	// negotiated back to the browser.
	Protocol string

	Verifier *authn.Verifier

	// Origins is the browser origin allow-list, the same list CORS uses. An
	// upgrade carries credentials, so a wildcard is not an option here either.
	Origins []string

	Logger *slog.Logger
}

// New returns a Handler.
func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Upstream == "":
		return nil, errors.New("wsproxy: an upstream address is required")
	case cfg.Protocol == "":
		return nil, errors.New("wsproxy: a subprotocol is required")
	case cfg.Verifier == nil:
		return nil, errors.New("wsproxy: token verifier is required")
	case len(cfg.Origins) == 0:
		return nil, errors.New("wsproxy: an origin allow-list is required")
	case cfg.Logger == nil:
		return nil, errors.New("wsproxy: logger is required")
	}

	return &Handler{
		upstream: cfg.Upstream,
		protocol: cfg.Protocol,
		verifier: cfg.Verifier,
		origins:  cfg.Origins,
		log:      cfg.Logger,
	}, nil
}

// ServeHTTP authenticates the upgrade, dials the service and joins the two.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := bearerFrom(r.Header)
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Verified here as well as upstream. Not redundant: this is what keeps an
	// unauthenticated caller from opening a connection to an internal service
	// at all, and it is the check that lets a client be told "unauthorized"
	// with a status code instead of a close frame.
	if _, err := h.verifier.Verify(token); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	client, err := ws.Accept(w, r, ws.AcceptOptions{
		// Only the conversation protocol is offered back. The bearer entry was
		// a transport for the token, not something either end speaks.
		Subprotocols:   []string{h.protocol},
		OriginPatterns: h.origins,
		Options:        ws.Options{Logger: h.log},
	})
	if err != nil {
		h.log.WarnContext(ctx, "websocket upgrade failed", "error", err)
		return
	}
	defer func() { _ = client.CloseNow() }()

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)

	upstream, err := ws.Dial(ctx, h.upstream, ws.DialOptions{
		Subprotocols: []string{h.protocol},
		Header:       header,
		Options:      ws.Options{Logger: h.log},
	})
	if err != nil {
		h.log.ErrorContext(ctx, "could not reach the realtime service",
			"upstream", h.upstream, "error", err)
		_ = client.Close(ws.StatusInternalError, "the service is unavailable")
		return
	}
	defer func() { _ = upstream.CloseNow() }()

	h.log.InfoContext(ctx, "realtime connection opened",
		"upstream", h.upstream, "correlation_id", correlation.FromContext(ctx))

	h.join(ctx, client, upstream)
}

// join copies frames in both directions until either side stops.
//
// The first end to finish decides how the other one is closed, so a client that
// hangs up releases the upstream connection immediately rather than leaving it
// for a keepalive to notice, and a close code the service chose — an expired
// token, say — reaches the browser as itself rather than as a generic hang-up.
func (h *Handler) join(ctx context.Context, client, upstream *ws.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{}, 2)

	go func() {
		h.pipe(ctx, client, upstream)
		done <- struct{}{}
	}()
	go func() {
		h.pipe(ctx, upstream, client)
		done <- struct{}{}
	}()

	<-done
	cancel()

	// The second copier is unblocked by the close its peer just took, but a
	// stuck write could outlive it. Waiting is bounded so one wedged connection
	// cannot pin this handler for the life of the process.
	select {
	case <-done:
	case <-time.After(closeGrace):
	}
}

// closeGrace bounds how long the second direction may take to notice the first
// one ended.
const closeGrace = 5 * time.Second

// pipe copies frames from one connection to the other, closing the destination
// the way the source was closed.
func (h *Handler) pipe(ctx context.Context, from, to *ws.Conn) {
	for {
		payload, err := from.Read(ctx)
		if err != nil {
			code, reason := closeFor(err)
			_ = to.Close(code, ws.TruncateReason(reason))
			return
		}

		if err := to.Write(ctx, payload); err != nil {
			// The other side is gone or too slow. Closing the source ends the
			// opposite copier too, which is what tears the pair down.
			_ = from.CloseNow()
			return
		}
	}
}

// closeFor turns the error that ended one side into the close to send on the
// other.
//
// An application code — anything at or above 4000 — is passed through
// unchanged. Those are the service's own vocabulary, and a client that is told
// "the connection ended" instead of "your token expired" has no idea whether
// reconnecting will help.
func closeFor(err error) (ws.StatusCode, string) {
	if code := ws.CloseStatus(err); code != -1 {
		return code, ""
	}
	return ws.CloseCodeFor(err)
}

// bearerFrom pulls the access token out of the offered subprotocols.
func bearerFrom(header http.Header) string {
	for _, value := range header.Values("Sec-WebSocket-Protocol") {
		for _, offer := range strings.Split(value, ",") {
			offer = strings.TrimSpace(offer)
			if token, ok := strings.CutPrefix(offer, BearerPrefix); ok && token != "" {
				return token
			}
		}
	}
	return ""
}

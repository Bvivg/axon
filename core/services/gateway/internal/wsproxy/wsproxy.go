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

const BearerPrefix = "axon.bearer."

type Handler struct {
	upstream string
	protocol string
	verifier *authn.Verifier
	origins  []string
	log      *slog.Logger
}

type Config struct {
	Upstream string

	Protocol string

	Verifier *authn.Verifier

	Origins []string

	Logger *slog.Logger
}

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

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := bearerFrom(r.Header)
	if token == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if _, err := h.verifier.Verify(token); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	client, err := ws.Accept(w, r, ws.AcceptOptions{

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

	select {
	case <-done:
	case <-time.After(closeGrace):
	}
}

const closeGrace = 5 * time.Second

func (h *Handler) pipe(ctx context.Context, from, to *ws.Conn) {
	for {
		payload, err := from.Read(ctx)
		if err != nil {
			code, reason := closeFor(err)
			_ = to.Close(code, ws.TruncateReason(reason))
			return
		}

		if err := to.Write(ctx, payload); err != nil {

			_ = from.CloseNow()
			return
		}
	}
}

func closeFor(err error) (ws.StatusCode, string) {
	if code := ws.CloseStatus(err); code != -1 {
		return code, ""
	}
	return ws.CloseCodeFor(err)
}

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

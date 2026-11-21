package wsproxy

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/gateway/internal/guard"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

const BearerPrefix = "axon.bearer."

type Inspect func(ctx context.Context, payload []byte) ([]byte, func())

type Handler struct {
	inspect       Inspect
	upstream      string
	protocol      string
	verifier      *authn.Verifier
	origins       []string
	log           *slog.Logger
	socketOptions ws.Options
	client        *http.Client
	limiter       *ratelimit.Limiter
	open          *openSockets
}

type Config struct {
	SocketOptions ws.Options

	Upstream string

	Protocol string

	Verifier *authn.Verifier

	Origins []string

	Logger *slog.Logger

	Inspect Inspect

	HTTPClient *http.Client

	Limiter *ratelimit.Limiter

	MaxPerUser int
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
		socketOptions: cfg.SocketOptions,
		inspect:       cfg.Inspect,
		upstream:      cfg.Upstream,
		protocol:      cfg.Protocol,
		verifier:      cfg.Verifier,
		origins:       cfg.Origins,
		log:           cfg.Logger,
		client:        cfg.HTTPClient,
		limiter:       cfg.Limiter,
		open:          &openSockets{limit: cfg.MaxPerUser, count: map[uuid.UUID]int{}},
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := bearerFrom(r.Header)
	claims, err := h.verifier.Verify(token)
	if err != nil {
		if h.admit(w, "ip:"+ratelimit.Subject(guard.ClientIPFromContext(ctx))) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}
		return
	}
	if !h.admit(w, "user:"+claims.UserID.String()) {
		return
	}
	release, ok := h.open.acquire(claims.UserID)
	if !ok {
		http.Error(w, "too many open connections", http.StatusTooManyRequests)
		return
	}
	defer release()

	options := h.socketOptions
	options.Logger = h.log
	client, err := ws.Accept(w, r, ws.AcceptOptions{

		Subprotocols:   []string{h.protocol},
		OriginPatterns: h.origins,
		Options:        options,
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
		Options:      options,
		HTTPClient:   h.client,
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

func (h *Handler) admit(w http.ResponseWriter, subject string) bool {
	if h.limiter == nil {
		return true
	}
	allowed, retryAfter := h.limiter.AllowWithRetry(subject)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	}
	return allowed
}

type openSockets struct {
	limit int
	mu    sync.Mutex
	count map[uuid.UUID]int
}

func (o *openSockets) acquire(user uuid.UUID) (func(), bool) {
	if o.limit <= 0 {
		return func() {}, true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.count[user] >= o.limit {
		return nil, false
	}
	o.count[user]++
	return sync.OnceFunc(func() {
		o.mu.Lock()
		defer o.mu.Unlock()
		if o.count[user]--; o.count[user] <= 0 {
			delete(o.count, user)
		}
	}), true
}

func (h *Handler) join(ctx context.Context, client, upstream *ws.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{}, 2)

	go func() {
		h.pipe(ctx, client, upstream, h.inspect)
		done <- struct{}{}
	}()
	go func() {
		h.pipe(ctx, upstream, client, nil)
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

func (h *Handler) pipe(ctx context.Context, from, to *ws.Conn, inspect Inspect) {
	for {
		payload, err := from.Read(ctx)
		if err != nil {
			code, reason := closeFor(err)
			_ = to.Close(code, ws.TruncateReason(reason))
			return
		}

		done := func() {}
		if inspect != nil {
			payload, done = inspect(ctx, payload)
		}
		err = to.Write(ctx, payload)
		done()
		if err != nil {
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

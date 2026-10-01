package attachmentproxy

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

const (
	MaxPartBytes = 16 << 20

	maxControlBytes = 4 << 10
)

type SessionsConfig struct {
	Upstream    string
	Verifier    *authn.Verifier
	Limiter     *ratelimit.Limiter
	PartLimiter *ratelimit.Limiter
	Client      *http.Client
	Logger      *slog.Logger
}

type Sessions struct {
	upstream    string
	verifier    *authn.Verifier
	limiter     *ratelimit.Limiter
	partLimiter *ratelimit.Limiter
	client      *http.Client
	log         *slog.Logger
}

func NewSessions(cfg SessionsConfig) (*Sessions, error) {
	switch {
	case cfg.Upstream == "":
		return nil, errors.New("attachmentproxy: an upstream address is required")
	case cfg.Verifier == nil:
		return nil, errors.New("attachmentproxy: token verifier is required")
	case cfg.Limiter == nil || cfg.PartLimiter == nil:
		return nil, errors.New("attachmentproxy: rate limiters are required")
	case cfg.Client == nil:
		return nil, errors.New("attachmentproxy: http client is required")
	case cfg.Logger == nil:
		return nil, errors.New("attachmentproxy: logger is required")
	}
	return &Sessions{
		upstream:    strings.TrimSuffix(cfg.Upstream, "/"),
		verifier:    cfg.Verifier,
		limiter:     cfg.Limiter,
		partLimiter: cfg.PartLimiter,
		client:      cfg.Client,
		log:         cfg.Logger,
	}, nil
}

func (h *Sessions) Register(mux *http.ServeMux, prefix string) {
	control := func(w http.ResponseWriter, r *http.Request) { h.forward(w, r, prefix, h.limiter, maxControlBytes) }
	part := func(w http.ResponseWriter, r *http.Request) { h.forward(w, r, prefix, h.partLimiter, MaxPartBytes) }

	mux.HandleFunc("POST "+prefix, control)
	mux.HandleFunc("PUT "+prefix+"/{id}/parts/{n}", part)
	mux.HandleFunc("GET "+prefix+"/{id}", control)
	mux.HandleFunc("POST "+prefix+"/{id}/complete", control)
	mux.HandleFunc("DELETE "+prefix+"/{id}", control)
}

func (h *Sessions) forward(w http.ResponseWriter, r *http.Request, prefix string, limiter *ratelimit.Limiter, maxBody int64) {
	token, err := authn.BearerToken(r.Header)
	if err != nil {
		http.Error(w, "no access token", http.StatusUnauthorized)
		return
	}
	claims, err := h.verifier.Verify(token)
	if err != nil {
		http.Error(w, "access token is not valid", http.StatusUnauthorized)
		return
	}
	if !limiter.Allow("user:" + claims.UserID.String()) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	target := h.upstream + strings.TrimPrefix(r.URL.Path, prefix)
	if wait := r.URL.Query().Get("wait"); wait != "" {
		target += "?" + url.Values{"wait": {wait}}.Encode()
	}

	var body io.Reader = http.NoBody
	if r.ContentLength > 0 {
		body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if r.ContentLength > 0 {
		req.ContentLength = r.ContentLength
	}

	resp, err := h.client.Do(req)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "the part is too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.log.ErrorContext(r.Context(), "could not reach chat for an upload session",
			"upstream", h.upstream, "method", r.Method, "error", err)
		http.Error(w, "the service is unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

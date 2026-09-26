package attachmentproxy

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

const MaxUploadBytes = 100 << 20

type Config struct {
	Upstream string
	Verifier *authn.Verifier
	Limiter  *ratelimit.Limiter
	Client   *http.Client
	Logger   *slog.Logger
}

type Handler struct {
	upstream string
	verifier *authn.Verifier
	limiter  *ratelimit.Limiter
	client   *http.Client
	log      *slog.Logger
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Upstream == "":
		return nil, errors.New("attachmentproxy: an upstream address is required")
	case cfg.Verifier == nil:
		return nil, errors.New("attachmentproxy: token verifier is required")
	case cfg.Limiter == nil:
		return nil, errors.New("attachmentproxy: rate limiter is required")
	case cfg.Client == nil:
		return nil, errors.New("attachmentproxy: http client is required")
	case cfg.Logger == nil:
		return nil, errors.New("attachmentproxy: logger is required")
	}

	return &Handler{
		upstream: cfg.Upstream,
		verifier: cfg.Verifier,
		limiter:  cfg.Limiter,
		client:   cfg.Client,
		log:      cfg.Logger,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	if !h.limiter.Allow("user:" + claims.UserID.String()) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)

	target := h.upstream
	if as := r.URL.Query().Get("as"); as != "" {
		target += "?" + url.Values{"as": {as}}.Encode()
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if filename := r.Header.Get("X-Filename"); filename != "" {
		req.Header.Set("X-Filename", filename)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.ContentLength = r.ContentLength

	resp, err := h.client.Do(req)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "the file is too large", http.StatusRequestEntityTooLarge)
			return
		}

		h.log.ErrorContext(r.Context(), "could not reach chat for an attachment upload",
			"upstream", h.upstream, "error", err)
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

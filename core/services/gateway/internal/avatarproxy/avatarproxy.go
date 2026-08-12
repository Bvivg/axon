package avatarproxy

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

const MaxUploadBytes = 5 << 20

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
		return nil, errors.New("avatarproxy: an upstream address is required")
	case cfg.Verifier == nil:
		return nil, errors.New("avatarproxy: token verifier is required")
	case cfg.Limiter == nil:
		return nil, errors.New("avatarproxy: rate limiter is required")
	case cfg.Client == nil:
		return nil, errors.New("avatarproxy: http client is required")
	case cfg.Logger == nil:
		return nil, errors.New("avatarproxy: logger is required")
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

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.upstream, r.Body)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.ContentLength = r.ContentLength

	resp, err := h.client.Do(req)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "the image is too large", http.StatusRequestEntityTooLarge)
			return
		}

		h.log.ErrorContext(r.Context(), "could not reach auth for an avatar upload",
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

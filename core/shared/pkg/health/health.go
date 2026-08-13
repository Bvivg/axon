package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

const (
	PathLive  = "/healthz"
	PathReady = "/readyz"
)

const defaultTimeout = 2 * time.Second

type Checker interface {
	Name() string

	Check(ctx context.Context) error
}

type CheckerFunc struct {
	CheckerName string
	Fn          func(ctx context.Context) error
}

func (c CheckerFunc) Name() string                    { return c.CheckerName }
func (c CheckerFunc) Check(ctx context.Context) error { return c.Fn(ctx) }

type Handler struct {
	logger   *slog.Logger
	timeout  time.Duration
	checkers []Checker
}

type Option func(*Handler)

func WithTimeout(d time.Duration) Option {
	return func(h *Handler) { h.timeout = d }
}

func New(log *slog.Logger, checkers []Checker, opts ...Option) *Handler {
	h := &Handler{logger: log, timeout: defaultTimeout, checkers: checkers}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(PathLive, h.Live)
	mux.HandleFunc(PathReady, h.Ready)
}

type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

const (
	statusOK   = "ok"
	statusDown = "unavailable"
)

func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: statusOK})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	results := make([]string, len(h.checkers))

	var wg sync.WaitGroup
	for i, c := range h.checkers {
		wg.Add(1)
		go func(i int, c Checker) {
			defer wg.Done()
			if err := c.Check(ctx); err != nil {
				results[i] = err.Error()
			}
		}(i, c)
	}
	wg.Wait()

	resp := response{Status: statusOK, Checks: make(map[string]string, len(h.checkers))}
	code := http.StatusOK

	for i, c := range h.checkers {
		if results[i] == "" {
			resp.Checks[c.Name()] = statusOK
			continue
		}
		resp.Checks[c.Name()] = results[i]
		resp.Status = statusDown
		code = http.StatusServiceUnavailable
	}

	if code != http.StatusOK && h.logger != nil {
		h.logger.WarnContext(ctx, "readiness probe failed", "checks", resp.Checks)
	}

	writeJSON(w, code, resp)
}

func writeJSON(w http.ResponseWriter, code int, body response) {
	w.Header().Set("Content-Type", "application/json")

	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// Package health exposes the two probes every Axon service serves.
//
// The split matters. /healthz is liveness: it answers "is this process still
// running" and must never touch a dependency, otherwise a brief database
// outage gets every replica killed and restarted. /readyz is readiness: it
// checks the dependencies the service genuinely cannot serve without, so an
// unready replica is taken out of rotation while staying alive.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Default probe paths.
const (
	PathLive  = "/healthz"
	PathReady = "/readyz"
)

// defaultTimeout bounds a whole readiness sweep. A probe that hangs is a probe
// that fails.
const defaultTimeout = 2 * time.Second

// Checker is a single dependency that readiness verifies. Implementations must
// be safe for concurrent use and must respect ctx.
type Checker interface {
	// Name identifies the dependency in the probe response, e.g. "postgres".
	Name() string
	// Check returns nil when the dependency is usable.
	Check(ctx context.Context) error
}

// CheckerFunc adapts a function to Checker.
type CheckerFunc struct {
	CheckerName string
	Fn          func(ctx context.Context) error
}

func (c CheckerFunc) Name() string                    { return c.CheckerName }
func (c CheckerFunc) Check(ctx context.Context) error { return c.Fn(ctx) }

// Handler serves the liveness and readiness probes.
type Handler struct {
	logger   *slog.Logger
	timeout  time.Duration
	checkers []Checker
}

// Option customises a Handler.
type Option func(*Handler)

// WithTimeout overrides the readiness sweep timeout.
func WithTimeout(d time.Duration) Option {
	return func(h *Handler) { h.timeout = d }
}

// New returns a Handler that reports ready once every checker passes.
func New(log *slog.Logger, checkers []Checker, opts ...Option) *Handler {
	h := &Handler{logger: log, timeout: defaultTimeout, checkers: checkers}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register mounts both probes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc(PathLive, h.Live)
	mux.HandleFunc(PathReady, h.Ready)
}

// response is the probe payload. Keeping it structured means a human reading
// a failing probe sees which dependency broke, not just a 503.
type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

const (
	statusOK   = "ok"
	statusDown = "unavailable"
)

// Live answers the liveness probe. It deliberately checks nothing.
func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, response{Status: statusOK})
}

// Ready runs every checker in parallel and reports the aggregate.
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
	// Probes must never be served from a cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

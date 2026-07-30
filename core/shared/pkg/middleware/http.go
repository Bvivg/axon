// Package middleware holds the cross-cutting request plumbing every Axon
// service shares: correlation ID propagation, panic recovery, request logging
// and RPC metrics.
//
// Everything comes in two flavours. Connect interceptors cover RPC, where the
// unit of work is a procedure and the outcome is a connect.Code. Plain
// net/http middleware covers what is not RPC — WebSocket upgrades, the metrics
// listener, OAuth redirect callbacks.
package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// Middleware is the standard net/http decorator shape.
type Middleware func(http.Handler) http.Handler

// Chain composes middleware so that Chain(a, b, c)(h) runs a, then b, then c,
// then h — reading order matches execution order.
func Chain(mw ...Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			next = mw[i](next)
		}
		return next
	}
}

// Correlation puts a correlation ID on the request context, reusing the
// inbound header when there is one and generating one otherwise. The ID is
// echoed back on the response so a client can quote it in a bug report.
//
// Trusting an inbound header is safe behind the gateway but not at the edge:
// the gateway is responsible for deciding whether a client-supplied ID may be
// adopted or must be replaced.
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, id := correlation.Ensure(correlation.WithID(r.Context(), r.Header.Get(correlation.Header)))
		w.Header().Set(correlation.Header, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Recovery turns a panic into a 500 and a log line with a stack trace, so one
// bad request cannot take the process down. The response body carries no
// detail: internals never cross the wire.
func Recovery(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// http.ErrAbortHandler is the documented way to abort a
				// response; propagating it keeps that contract intact.
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}

				log.ErrorContext(r.Context(), "panic recovered",
					"panic", v,
					"method", r.Method,
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogger logs one line per request once it completes.
func RequestLogger(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			if rec.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}

			log.Log(r.Context(), level, "http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// statusRecorder remembers the status code so the logger can report it.
type statusRecorder struct {
	http.ResponseWriter

	status  int
	written bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.status = status
	r.written = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer, which
// WebSocket upgrades and streaming responses need.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

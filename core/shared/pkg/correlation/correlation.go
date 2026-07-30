// Package correlation carries a request-scoped correlation ID through
// context.Context so that every log line, metric label and downstream call
// can be tied back to the request that caused it.
//
// The ID enters the system at the trust boundary (the gateway) and is
// propagated verbatim to every service behind it. Services never invent a
// new ID for a request that already has one.
package correlation

import (
	"context"

	"github.com/google/uuid"
)

// Header is the wire representation of the correlation ID. The same spelling
// is used for HTTP headers and for Connect/gRPC metadata, which is why it is
// canonical-cased: net/http canonicalises on write, and Connect headers are
// http.Header underneath.
const Header = "X-Correlation-Id"

// ctxKey is unexported so no other package can collide with or overwrite the
// value we store.
type ctxKey struct{}

// NewID returns a fresh correlation ID.
func NewID() string {
	return uuid.NewString()
}

// WithID returns a copy of ctx carrying id. An empty id is ignored: callers
// get ctx back unchanged rather than a context that reports an empty ID,
// which keeps FromContext's "" result unambiguous.
func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the correlation ID stored in ctx, or "" when the
// context does not carry one.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// Ensure returns a context that is guaranteed to carry a correlation ID,
// generating one only when ctx does not already have it. Middleware at the
// edge uses this; inner layers should use FromContext so a missing ID stays
// visible instead of being silently papered over.
func Ensure(ctx context.Context) (context.Context, string) {
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := NewID()
	return WithID(ctx, id), id
}

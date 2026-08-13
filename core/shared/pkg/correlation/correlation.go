package correlation

import (
	"context"

	"github.com/google/uuid"
)

const Header = "X-Correlation-Id"

type ctxKey struct{}

func NewID() string {
	return uuid.NewString()
}

func WithID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func Ensure(ctx context.Context) (context.Context, string) {
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := NewID()
	return WithID(ctx, id), id
}

package correlation

import (
	"context"
	"strings"

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

type echoedKey struct{}

func MarkEchoed(ctx context.Context) context.Context {
	return context.WithValue(ctx, echoedKey{}, true)
}

func Echoed(ctx context.Context) bool {
	echoed, _ := ctx.Value(echoedKey{}).(bool)
	return echoed
}

func Ensure(ctx context.Context) (context.Context, string) {
	if id := FromContext(ctx); id != "" {
		return ctx, id
	}
	id := NewID()
	return WithID(ctx, id), id
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZabcdefghjkmnpqrstvwxyz"

func Valid(id string) bool {
	switch len(id) {
	case 36:
		return canonicalUUID(id)
	case 26:
		return ulid(id)
	}
	return false
}

func FromHeader(value string) string {
	if Valid(value) {
		return value
	}
	return ""
}

func canonicalUUID(id string) bool {
	for i := range len(id) {
		c := id[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !hexDigit(c) {
				return false
			}
		}
	}
	return true
}

func hexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func ulid(id string) bool {
	if id[0] < '0' || id[0] > '7' {
		return false
	}
	for i := range len(id) {
		if !strings.ContainsRune(crockford, rune(id[i])) {
			return false
		}
	}
	return true
}

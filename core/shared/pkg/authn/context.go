package authn

import (
	"context"
	"net/http"
	"strings"
)

const Header = "Authorization"

const bearerPrefix = "bearer "

type ctxKey struct{}

func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, claims)
}

func FromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(ctxKey{}).(Claims)
	return claims, ok
}

func BearerToken(headers http.Header) (string, error) {
	raw := headers.Get(Header)
	if raw == "" {
		return "", ErrNoToken
	}

	if len(raw) < len(bearerPrefix) || !strings.EqualFold(raw[:len(bearerPrefix)], bearerPrefix) {
		return "", ErrInvalidToken
	}

	token := strings.TrimSpace(raw[len(bearerPrefix):])
	if token == "" {
		return "", ErrInvalidToken
	}
	return token, nil
}

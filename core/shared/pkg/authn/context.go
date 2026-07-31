package authn

import (
	"context"
	"net/http"
	"strings"
)

// Header is where the access token arrives.
const Header = "Authorization"

// bearerPrefix is the scheme. RFC 7235 makes the scheme token case-insensitive
// and clients differ, so it is matched that way.
const bearerPrefix = "bearer "

type ctxKey struct{}

// WithClaims returns a copy of ctx carrying verified claims.
//
// Only code that has actually verified a token should call this: everything
// downstream treats the presence of claims as proof the request is authenticated.
func WithClaims(ctx context.Context, claims Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, claims)
}

// FromContext returns the claims attached to ctx, if any.
func FromContext(ctx context.Context) (Claims, bool) {
	claims, ok := ctx.Value(ctxKey{}).(Claims)
	return claims, ok
}

// BearerToken extracts the token from an Authorization header.
//
// A missing header and a malformed one are told apart: the first is an
// unauthenticated request, which for an optional-auth route is normal, while the
// second is a client bug worth reporting as such.
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

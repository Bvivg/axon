// Package guard enforces the gateway's policy on inbound calls: who may call a
// procedure, and how often.
//
// It is the trust boundary. Everything behind the gateway is internal traffic
// that has already been through here — which is also why services still do their
// own resource-level authorization: passing through here proves who the caller
// is, not that they may touch a particular thing.
package guard

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/gateway/internal/policy"
	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
)

// clientIPKey carries the caller's address from the HTTP layer, where it is
// known, into the interceptor, where Connect no longer exposes the connection.
type clientIPKey struct{}

// WithClientIP attaches the caller's address to ctx.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// ClientIPFromContext returns the address attached by the HTTP middleware.
func ClientIPFromContext(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// ClientIPMiddleware records the caller's address on the request context.
func ClientIPMiddleware(trustedProxies int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ratelimit.ClientIP(r, trustedProxies)
			next.ServeHTTP(w, r.WithContext(WithClientIP(r.Context(), ip)))
		})
	}
}

// Interceptor applies the policy to every call.
type Interceptor struct {
	verifier  *authn.Verifier
	standard  *ratelimit.Limiter
	sensitive *ratelimit.Limiter
	log       *slog.Logger
}

var _ connect.Interceptor = (*Interceptor)(nil)

// Config configures an Interceptor.
type Config struct {
	Verifier *authn.Verifier

	// Standard and Sensitive are the two budgets the policy selects between.
	Standard  *ratelimit.Limiter
	Sensitive *ratelimit.Limiter

	Logger *slog.Logger
}

// New validates the dependencies and returns an Interceptor.
func New(cfg Config) (*Interceptor, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("guard: verifier is required")
	case cfg.Standard == nil || cfg.Sensitive == nil:
		return nil, errors.New("guard: both rate limiters are required")
	case cfg.Logger == nil:
		return nil, errors.New("guard: logger is required")
	}

	return &Interceptor{
		verifier:  cfg.Verifier,
		standard:  cfg.Standard,
		sensitive: cfg.Sensitive,
		log:       cfg.Logger,
	}, nil
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// Client-side calls are the gateway talking to a service; the policy is
		// about what arrives from outside.
		if req.Spec().IsClient {
			return next(ctx, req)
		}

		ctx, err := i.apply(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.apply(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

// apply authenticates the caller if the procedure requires it, then charges the
// appropriate budget.
//
// The order matters. Authentication comes first so an authenticated caller is
// limited by account rather than by address: otherwise everyone behind one
// corporate NAT shares a budget, and a single user on a phone gets a new one
// every time their address changes.
func (i *Interceptor) apply(ctx context.Context, procedure string, headers http.Header) (context.Context, error) {
	rule := policy.For(procedure)

	ctx, subject, err := i.authenticate(ctx, rule, headers)
	if err != nil {
		return ctx, err
	}

	limiter := i.standard
	if rule.Tier == policy.TierSensitive {
		limiter = i.sensitive
	}

	if !limiter.Allow(subject) {
		i.log.WarnContext(ctx, "rate limit exceeded",
			"procedure", procedure,
			"tier", rule.Tier,
		)
		return ctx, connect.NewError(connect.CodeResourceExhausted,
			errors.New("too many requests"))
	}

	return ctx, nil
}

// authenticate verifies the token when the procedure demands one, and returns
// the key the caller is limited by.
func (i *Interceptor) authenticate(ctx context.Context, rule policy.Rule, headers http.Header) (context.Context, string, error) {
	raw, err := authn.BearerToken(headers)

	switch {
	case errors.Is(err, authn.ErrNoToken):
		if !rule.Public {
			return ctx, "", connect.NewError(connect.CodeUnauthenticated,
				errors.New("no access token"))
		}
		// Anonymous call to a public procedure: limited by address.
		return ctx, "ip:" + ClientIPFromContext(ctx), nil

	case err != nil:
		// A malformed header is a client bug, and saying so is safe: it reveals
		// nothing about whether any token would have worked.
		return ctx, "", connect.NewError(connect.CodeUnauthenticated,
			errors.New("authorization header is not a bearer token"))
	}

	claims, err := i.verifier.Verify(raw)
	if err != nil {
		if !rule.Public {
			return ctx, "", translateTokenError(err)
		}
		// A bad token on a public procedure is not a reason to refuse — the
		// procedure did not need one. It is a reason not to credit the caller
		// with an identity they failed to prove.
		return ctx, "ip:" + ClientIPFromContext(ctx), nil
	}

	// Claims go on the context so downstream handlers do not re-parse the token,
	// and the caller is limited by account.
	return authn.WithClaims(ctx, claims), "user:" + claims.UserID.String(), nil
}

// translateTokenError maps a verification failure onto a Connect code.
//
// Expiry is distinguishable because the client's move is different: refresh,
// rather than sign in again. Everything else is one opaque answer.
func translateTokenError(err error) error {
	if errors.Is(err, authn.ErrExpired) {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token expired"))
	}
	return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))
}

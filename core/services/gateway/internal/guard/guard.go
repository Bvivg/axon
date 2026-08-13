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

type clientIPKey struct{}

func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

func ClientIPFromContext(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

func ClientIPMiddleware(trustedProxies int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ratelimit.ClientIP(r, trustedProxies)
			next.ServeHTTP(w, r.WithContext(WithClientIP(r.Context(), ip)))
		})
	}
}

type Interceptor struct {
	verifier  *authn.Verifier
	standard  *ratelimit.Limiter
	sensitive *ratelimit.Limiter
	log       *slog.Logger
}

var _ connect.Interceptor = (*Interceptor)(nil)

type Config struct {
	Verifier *authn.Verifier

	Standard  *ratelimit.Limiter
	Sensitive *ratelimit.Limiter

	Logger *slog.Logger
}

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

func (i *Interceptor) authenticate(ctx context.Context, rule policy.Rule, headers http.Header) (context.Context, string, error) {
	raw, err := authn.BearerToken(headers)

	switch {
	case errors.Is(err, authn.ErrNoToken):
		if !rule.Public {
			return ctx, "", connect.NewError(connect.CodeUnauthenticated,
				errors.New("no access token"))
		}

		return ctx, "ip:" + ClientIPFromContext(ctx), nil

	case err != nil:

		return ctx, "", connect.NewError(connect.CodeUnauthenticated,
			errors.New("authorization header is not a bearer token"))
	}

	claims, err := i.verifier.Verify(raw)
	if err != nil {
		if !rule.Public {
			return ctx, "", translateTokenError(err)
		}

		return ctx, "ip:" + ClientIPFromContext(ctx), nil
	}

	return authn.WithClaims(ctx, claims), "user:" + claims.UserID.String(), nil
}

func translateTokenError(err error) error {
	if errors.Is(err, authn.ErrExpired) {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token expired"))
	}
	return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))
}

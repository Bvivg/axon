package guard

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

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
	search    *ratelimit.Limiter
	standard  *ratelimit.Limiter
	sensitive *ratelimit.Limiter
	network   *ratelimit.Limiter
	log       *slog.Logger
}

const DeviceHeader = "X-Axon-Device"

var _ connect.Interceptor = (*Interceptor)(nil)

type Config struct {
	Verifier *authn.Verifier

	Search    *ratelimit.Limiter
	Standard  *ratelimit.Limiter
	Sensitive *ratelimit.Limiter
	Network   *ratelimit.Limiter

	Logger *slog.Logger
}

func New(cfg Config) (*Interceptor, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("guard: verifier is required")
	case cfg.Search == nil || cfg.Standard == nil || cfg.Sensitive == nil:
		return nil, errors.New("guard: all rate limiters are required")
	case cfg.Logger == nil:
		return nil, errors.New("guard: logger is required")
	}

	return &Interceptor{
		verifier:  cfg.Verifier,
		search:    cfg.Search,
		standard:  cfg.Standard,
		sensitive: cfg.Sensitive,
		network:   cfg.Network,
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

	if rule.Tier == policy.TierSearch {
		limiter = i.search
	}

	allowed, retryAfter := limiter.AllowWithRetry(subject)
	if allowed && i.network != nil && rule.Tier == policy.TierSensitive && strings.HasPrefix(subject, "ip:") {
		allowed, retryAfter = i.network.AllowWithRetry("ip:" + ClientIPFromContext(ctx))
	}
	if !allowed {
		i.log.WarnContext(ctx, "rate limit exceeded",
			"procedure", procedure,
			"tier", rule.Tier,
		)
		err := connect.NewError(connect.CodeResourceExhausted, errors.New("too many requests"))
		err.Meta().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retryAfter.Seconds())))))
		return ctx, err
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

		return ctx, anonymous(ctx, headers), nil

	case err != nil:

		return ctx, "", connect.NewError(connect.CodeUnauthenticated,
			errors.New("authorization header is not a bearer token"))
	}

	claims, err := i.verifier.Verify(raw)
	if err != nil {
		if !rule.Public {
			return ctx, "", translateTokenError(err)
		}

		return ctx, anonymous(ctx, headers), nil
	}

	return authn.WithClaims(ctx, claims), "user:" + claims.UserID.String(), nil
}

func anonymous(ctx context.Context, headers http.Header) string {
	subject := "ip:" + ClientIPFromContext(ctx)
	if device, err := uuid.Parse(headers.Get(DeviceHeader)); err == nil && device != uuid.Nil {
		subject += ":device:" + device.String()
	}
	return subject
}

func translateTokenError(err error) error {
	if errors.Is(err, authn.ErrExpired) {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("access token expired"))
	}
	return connect.NewError(connect.CodeUnauthenticated, errors.New("access token is not valid"))
}

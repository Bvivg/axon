package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
)

type Resolver struct {
	client  authv1connect.AuthServiceClient
	timeout time.Duration
	log     *slog.Logger
}

type Config struct {
	HTTPClient connect.HTTPClient

	BaseURL string

	Timeout time.Duration

	Logger *slog.Logger
}

func New(cfg Config) (*Resolver, error) {
	switch {
	case cfg.HTTPClient == nil:
		return nil, errors.New("identity: http client is required")
	case cfg.BaseURL == "":
		return nil, errors.New("identity: auth service URL is required")
	case cfg.Logger == nil:
		return nil, errors.New("identity: logger is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &Resolver{
		client: authv1connect.NewAuthServiceClient(cfg.HTTPClient, cfg.BaseURL,
			connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
		),
		timeout: timeout,
		log:     cfg.Logger,
	}, nil
}

func (r *Resolver) DisplayName(ctx context.Context, accessToken string) string {
	if accessToken == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := r.client.GetMe(ctx, req)
	if err != nil {
		r.log.WarnContext(ctx, "could not read the caller's profile", "error", err)
		return ""
	}

	return resp.Msg.GetUser().GetDisplayName()
}

func BearerFromHeader(h http.Header) string {
	raw, err := authn.BearerToken(h)
	if err != nil {
		return ""
	}
	return raw
}

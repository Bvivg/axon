package proxy

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/gateway/internal/guard"
)

type Auth struct {
	client authv1connect.AuthServiceClient
}

var _ authv1connect.AuthServiceHandler = (*Auth)(nil)

func NewAuth(client authv1connect.AuthServiceClient) (*Auth, error) {
	if client == nil {
		return nil, errors.New("proxy: auth client is required")
	}
	return &Auth{client: client}, nil
}

func (a *Auth) Register(
	ctx context.Context,
	req *connect.Request[authv1.RegisterRequest],
) (*connect.Response[authv1.RegisterResponse], error) {
	return a.client.Register(ctx, forward(ctx, req))
}

func (a *Auth) Login(
	ctx context.Context,
	req *connect.Request[authv1.LoginRequest],
) (*connect.Response[authv1.LoginResponse], error) {
	return a.client.Login(ctx, forward(ctx, req))
}

func (a *Auth) RefreshToken(
	ctx context.Context,
	req *connect.Request[authv1.RefreshTokenRequest],
) (*connect.Response[authv1.RefreshTokenResponse], error) {
	return a.client.RefreshToken(ctx, forward(ctx, req))
}

func (a *Auth) Logout(
	ctx context.Context,
	req *connect.Request[authv1.LogoutRequest],
) (*connect.Response[authv1.LogoutResponse], error) {
	return a.client.Logout(ctx, forward(ctx, req))
}

func (a *Auth) GetMe(
	ctx context.Context,
	req *connect.Request[authv1.GetMeRequest],
) (*connect.Response[authv1.GetMeResponse], error) {
	return a.client.GetMe(ctx, forward(ctx, req))
}

func (a *Auth) StartOAuth(
	ctx context.Context,
	req *connect.Request[authv1.StartOAuthRequest],
) (*connect.Response[authv1.StartOAuthResponse], error) {
	return a.client.StartOAuth(ctx, forward(ctx, req))
}

func (a *Auth) CompleteOAuth(
	ctx context.Context,
	req *connect.Request[authv1.CompleteOAuthRequest],
) (*connect.Response[authv1.CompleteOAuthResponse], error) {
	return a.client.CompleteOAuth(ctx, forward(ctx, req))
}

func (a *Auth) UpdateProfile(
	ctx context.Context,
	req *connect.Request[authv1.UpdateProfileRequest],
) (*connect.Response[authv1.UpdateProfileResponse], error) {
	return a.client.UpdateProfile(ctx, forward(ctx, req))
}

func (a *Auth) ListSessions(
	ctx context.Context,
	req *connect.Request[authv1.ListSessionsRequest],
) (*connect.Response[authv1.ListSessionsResponse], error) {
	return a.client.ListSessions(ctx, forward(ctx, req))
}

func (a *Auth) RevokeSession(
	ctx context.Context,
	req *connect.Request[authv1.RevokeSessionRequest],
) (*connect.Response[authv1.RevokeSessionResponse], error) {
	return a.client.RevokeSession(ctx, forward(ctx, req))
}

func forward[T any](ctx context.Context, req *connect.Request[T]) *connect.Request[T] {
	out := connect.NewRequest(req.Msg)

	if v := req.Header().Get(authn.Header); v != "" {
		out.Header().Set(authn.Header, v)
	}

	if v := req.Header().Get("User-Agent"); v != "" {
		out.Header().Set("User-Agent", v)
	}

	if ip := guard.ClientIPFromContext(ctx); ip != "" {
		out.Header().Set(authn.ClientIPHeader, ip)
	}

	return out
}

func NewClient(httpClient *http.Client, baseURL string, opts ...connect.ClientOption) authv1connect.AuthServiceClient {
	return authv1connect.NewAuthServiceClient(httpClient, baseURL, opts...)
}

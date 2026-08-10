// Package proxy forwards the public contract to the services behind the gateway.
//
// The gateway implements the same generated interface it exposes, rather than
// blindly reverse-proxying bytes. That is what makes per-procedure policy
// possible at all: a byte proxy cannot tell Login from GetMe, so it cannot give
// them different rate limits or different authentication requirements.
package proxy

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

// Auth forwards AuthService calls to the auth service.
type Auth struct {
	client authv1connect.AuthServiceClient
}

var _ authv1connect.AuthServiceHandler = (*Auth)(nil)

// NewAuth returns a forwarding handler over the given client.
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

// forward builds the outbound request from the inbound one.
//
// Only headers that were deliberately chosen travel onward. Copying the whole
// inbound header set would carry Cookie, Origin, Referer and anything else a
// browser attached into the internal network — and give a caller a way to inject
// headers the downstream service might read.
func forward[T any](ctx context.Context, req *connect.Request[T]) *connect.Request[T] {
	out := connect.NewRequest(req.Msg)

	// The access token goes through so the service can verify it itself. The
	// gateway proving who the caller is does not relieve a service of checking
	// whether they may touch a particular resource, and it cannot check without
	// the token.
	if v := req.Header().Get(authn.Header); v != "" {
		out.Header().Set(authn.Header, v)
	}

	// The correlation interceptor on the client side writes the id from ctx, so
	// nothing is copied for it here.
	_ = ctx

	return out
}

// NewClient builds the Connect client the gateway forwards through.
func NewClient(httpClient *http.Client, baseURL string, opts ...connect.ClientOption) authv1connect.AuthServiceClient {
	return authv1connect.NewAuthServiceClient(httpClient, baseURL, opts...)
}

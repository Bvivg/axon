package server

import (
	"context"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type AuthRoles struct {
	client authv1connect.AuthServiceClient
}

func NewAuthRoles(client connect.HTTPClient, url string, opts ...connect.ClientOption) *AuthRoles {
	return &AuthRoles{client: authv1connect.NewAuthServiceClient(client, url, opts...)}
}

func (a *AuthRoles) Roles(ctx context.Context, authorization string) ([]string, error) {
	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set(authn.Header, authorization)
	res, err := a.client.GetMe(ctx, req)
	if err != nil {
		return nil, err
	}
	return res.Msg.GetUser().GetRoles(), nil
}

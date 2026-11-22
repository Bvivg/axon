package authz

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type Remote struct {
	client authv1connect.AuthAdminServiceClient
}

func NewRemote(client connect.HTTPClient, url string, opts ...connect.ClientOption) *Remote {
	return &Remote{client: authv1connect.NewAuthAdminServiceClient(client, url, opts...)}
}

func (r *Remote) Permissions(ctx context.Context, _ uuid.UUID, authorization string) ([]string, error) {
	req := connect.NewRequest(&authv1.GetMyPermissionsRequest{})
	req.Header().Set(authn.Header, authorization)
	res, err := r.client.GetMyPermissions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("authz: load permissions: %w", err)
	}
	return res.Msg.GetPermissions(), nil
}

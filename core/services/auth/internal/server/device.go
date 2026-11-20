package server

import (
	"context"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func DeviceInterceptor() connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			return next(domain.ContextWithDevice(ctx, deviceFrom(req.Header())), req)
		}
	}
}

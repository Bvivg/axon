package server_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
	"github.com/bvivg/axon/core/services/auth/internal/server"
)

func TestDeviceInterceptorCarriesTheForwardedDevice(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    domain.Device
	}{
		{
			name:    "forwarded by the gateway",
			headers: map[string]string{"User-Agent": "Firefox/140", authn.ClientIPHeader: "203.0.113.7"},
			want:    domain.Device{UserAgent: "Firefox/140", IP: "203.0.113.7"},
		},
		{
			name: "nothing forwarded",
			want: domain.Device{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := connect.NewRequest(&authv1.LogoutRequest{})
			for k, v := range tt.headers {
				req.Header().Set(k, v)
			}

			var got domain.Device
			next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
				got = domain.DeviceFromContext(ctx)
				return connect.NewResponse(&authv1.LogoutResponse{}), nil
			}

			if _, err := server.DeviceInterceptor()(next)(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("device = %+v, want %+v", got, tt.want)
			}
		})
	}
}

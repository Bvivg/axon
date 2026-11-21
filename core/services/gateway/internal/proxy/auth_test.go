package proxy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"

	"github.com/bvivg/axon/core/services/gateway/internal/proxy"
)

type busyAuth struct {
	authv1connect.UnimplementedAuthServiceHandler
}

func (busyAuth) Login(context.Context, *connect.Request[authv1.LoginRequest]) (*connect.Response[authv1.LoginResponse], error) {
	return nil, refusal(connect.CodeResourceExhausted, "too many failed sign-ins for this account; try again later", "42")
}

func (busyAuth) Register(context.Context, *connect.Request[authv1.RegisterRequest]) (*connect.Response[authv1.RegisterResponse], error) {
	return nil, refusal(connect.CodeResourceExhausted, "the service is busy; try again shortly", "1")
}

func (busyAuth) GetMe(context.Context, *connect.Request[authv1.GetMeRequest]) (*connect.Response[authv1.GetMeResponse], error) {
	return nil, refusal(connect.CodeUnauthenticated, "sign in again", "")
}

func refusal(code connect.Code, message, retryAfter string) error {
	err := connect.NewError(code, errors.New(message))
	if retryAfter != "" {
		err.Meta().Set("Retry-After", retryAfter)
	}
	err.Meta().Set("X-Upstream-Internal", "auth-1")
	return err
}

func serve(t *testing.T, handler authv1connect.AuthServiceHandler) *httptest.Server {
	t.Helper()

	path, h := authv1connect.NewAuthServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestTheGatewayTellsTheClientWhenARefusedSignInMayRetry(t *testing.T) {
	upstream := serve(t, busyAuth{})
	gateway, err := proxy.NewAuth(proxy.NewClient(upstream.Client(), upstream.URL))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	edge := serve(t, gateway)
	client := authv1connect.NewAuthServiceClient(edge.Client(), edge.URL)

	calls := map[string]struct {
		call       func() error
		code       connect.Code
		retryAfter string
	}{
		"locked account": {
			call: func() error {
				_, err := client.Login(t.Context(), connect.NewRequest(&authv1.LoginRequest{Email: "ada@example.com", Password: "x"}))
				return err
			},
			code:       connect.CodeResourceExhausted,
			retryAfter: "42",
		},
		"hashing at capacity": {
			call: func() error {
				_, err := client.Register(t.Context(), connect.NewRequest(&authv1.RegisterRequest{Email: "ada@example.com", Password: "x"}))
				return err
			},
			code:       connect.CodeResourceExhausted,
			retryAfter: "1",
		},
		"plain refusal": {
			call: func() error {
				_, err := client.GetMe(t.Context(), connect.NewRequest(&authv1.GetMeRequest{}))
				return err
			},
			code: connect.CodeUnauthenticated,
		},
	}

	for name, tc := range calls {
		t.Run(name, func(t *testing.T) {
			var got *connect.Error
			if !errors.As(tc.call(), &got) {
				t.Fatal("the call succeeded, want the upstream refusal")
			}
			if got.Code() != tc.code {
				t.Errorf("code = %v, want %v", got.Code(), tc.code)
			}
			if v := got.Meta().Get("Retry-After"); v != tc.retryAfter {
				t.Errorf("Retry-After = %q, want %q", v, tc.retryAfter)
			}
			if v := got.Meta().Get("X-Upstream-Internal"); v != "" {
				t.Errorf("an upstream header leaked to the client: %q", v)
			}
		})
	}
}

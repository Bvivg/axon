//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

var nextCaller atomic.Uint32

type caller struct {
	client authv1connect.AuthServiceClient
	addr   string
}

func newCaller(t *testing.T) *caller {
	t.Helper()

	n := nextCaller.Add(1)

	addr := fmt.Sprintf("10.%d.%d.%d", (n>>16)&0xff, (n>>8)&0xff, n&0xff)

	c := &caller{addr: addr}
	c.client = authv1connect.NewAuthServiceClient(
		&http.Client{Transport: forwardedFor{addr: addr}},
		gatewayURL,
	)
	return c
}

type forwardedFor struct {
	addr string
}

func (f forwardedFor) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Forwarded-For", f.addr)
	return http.DefaultTransport.RoundTrip(req)
}

func email() string {
	return "e2e-" + uuid.NewString() + "@axon.test"
}

const testPassword = "correct-horse-battery-staple"

type account struct {
	email  string
	userID string
	tokens *authv1.TokenPair
}

func (c *caller) register(t *testing.T) *account {
	t.Helper()

	addr := email()
	acct, err := c.registerAs(addr)
	if err != nil {
		t.Fatalf("register %s: %v", addr, err)
	}
	return acct
}

func (c *caller) registerAs(addr string) (*account, error) {
	displayName := "E2E User"

	resp, err := c.client.Register(context.Background(), connect.NewRequest(&authv1.RegisterRequest{
		Email:       addr,
		Password:    testPassword,
		DisplayName: &displayName,
	}))
	if err != nil {
		return nil, err
	}

	return &account{
		email:  addr,
		userID: resp.Msg.GetUser().GetId(),
		tokens: resp.Msg.GetTokens(),
	}, nil
}

func (c *caller) login(email, password string) (*authv1.TokenPair, error) {
	resp, err := c.client.Login(context.Background(), connect.NewRequest(&authv1.LoginRequest{
		Email:    email,
		Password: password,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTokens(), nil
}

func (c *caller) getMe(accessToken string) (*authv1.User, error) {
	req := connect.NewRequest(&authv1.GetMeRequest{})
	if accessToken != "" {
		req.Header().Set(authn.Header, "Bearer "+accessToken)
	}

	resp, err := c.client.GetMe(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetUser(), nil
}

func (c *caller) refresh(refreshToken string) (*authv1.TokenPair, error) {
	resp, err := c.client.RefreshToken(context.Background(), connect.NewRequest(&authv1.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTokens(), nil
}

func (c *caller) logout(refreshToken string) error {
	_, err := c.client.Logout(context.Background(), connect.NewRequest(&authv1.LogoutRequest{
		RefreshToken: refreshToken,
	}))
	return err
}

func requireCode(t *testing.T, err error, want connect.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected %v, got success", want)
	}
	if got := connect.CodeOf(err); got != want {
		t.Fatalf("code = %v, want %v (error: %v)", got, want, err)
	}
}

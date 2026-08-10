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

// nextCaller hands out a distinct forged address per caller.
//
// Every request in a run leaves the same container, so without this the first
// scenario to exhaust a rate-limit budget would fail all the others. The e2e
// gateway is configured with TRUSTED_PROXIES=1 for exactly this, which also
// means the trusted-proxy path through ratelimit.ClientIP is under test rather
// than assumed — that is how the gateway will run behind an ingress.
var nextCaller atomic.Uint32

// caller is one client of the gateway, with its own rate-limit bucket.
type caller struct {
	client authv1connect.AuthServiceClient
	addr   string
}

// newCaller returns a client whose requests are attributed to an address no
// other caller uses.
func newCaller(t *testing.T) *caller {
	t.Helper()

	n := nextCaller.Add(1)
	// 10.0.0.0/8 is private and unroutable: nothing here reaches the internet,
	// and an address that escaped into a log is obviously synthetic.
	addr := fmt.Sprintf("10.%d.%d.%d", (n>>16)&0xff, (n>>8)&0xff, n&0xff)

	c := &caller{addr: addr}
	c.client = authv1connect.NewAuthServiceClient(
		&http.Client{Transport: forwardedFor{addr: addr}},
		gatewayURL,
	)
	return c
}

// forwardedFor stamps the caller's address on every request.
type forwardedFor struct {
	addr string
}

func (f forwardedFor) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Forwarded-For", f.addr)
	return http.DefaultTransport.RoundTrip(req)
}

// email returns an address no other test will use. Registrations accumulate for
// the life of the stack, so a fixed address would make the second run of a test
// fail on a conflict the test never intended to create.
func email() string {
	return "e2e-" + uuid.NewString() + "@axon.test"
}

const testPassword = "correct-horse-battery-staple"

// account is a registered user and the tokens their registration produced.
type account struct {
	email  string
	userID string
	tokens *authv1.TokenPair
}

// register creates an account and fails the test if it cannot.
func (c *caller) register(t *testing.T) *account {
	t.Helper()

	addr := email()
	acct, err := c.registerAs(addr)
	if err != nil {
		t.Fatalf("register %s: %v", addr, err)
	}
	return acct
}

// registerAs registers a specific address and returns the error unchanged, for
// the tests that are about what the error is.
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

// login signs in and returns the error unchanged, because most callers are
// testing what the error is.
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

// getMe calls the protected procedure with the given access token. An empty
// token means none is sent at all.
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

// refresh exchanges a refresh token for a new pair.
func (c *caller) refresh(refreshToken string) (*authv1.TokenPair, error) {
	resp, err := c.client.RefreshToken(context.Background(), connect.NewRequest(&authv1.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg.GetTokens(), nil
}

// logout revokes a refresh token.
func (c *caller) logout(refreshToken string) error {
	_, err := c.client.Logout(context.Background(), connect.NewRequest(&authv1.LogoutRequest{
		RefreshToken: refreshToken,
	}))
	return err
}

// requireCode fails unless err carries the expected Connect code.
func requireCode(t *testing.T, err error, want connect.Code) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected %v, got success", want)
	}
	if got := connect.CodeOf(err); got != want {
		t.Fatalf("code = %v, want %v (error: %v)", got, want, err)
	}
}

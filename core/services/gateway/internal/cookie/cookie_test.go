package cookie_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"

	"github.com/bvivg/axon/core/services/gateway/internal/cookie"
)

const issuedToken = "refresh-token-from-auth"

type stub struct {
	authv1connect.UnimplementedAuthServiceHandler

	seen string

	fail bool
}

func (s *stub) tokens() *authv1.TokenPair {
	return &authv1.TokenPair{
		AccessToken:  "access-token",
		RefreshToken: issuedToken,
		ExpiresIn:    900,
		TokenType:    "Bearer",
	}
}

func (s *stub) Register(
	context.Context, *connect.Request[authv1.RegisterRequest],
) (*connect.Response[authv1.RegisterResponse], error) {
	if s.fail {
		return nil, connect.NewError(connect.CodeAlreadyExists, errStub)
	}
	return connect.NewResponse(&authv1.RegisterResponse{Tokens: s.tokens()}), nil
}

func (s *stub) Login(
	context.Context, *connect.Request[authv1.LoginRequest],
) (*connect.Response[authv1.LoginResponse], error) {
	if s.fail {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStub)
	}
	return connect.NewResponse(&authv1.LoginResponse{Tokens: s.tokens()}), nil
}

func (s *stub) CompleteOAuth(
	context.Context, *connect.Request[authv1.CompleteOAuthRequest],
) (*connect.Response[authv1.CompleteOAuthResponse], error) {
	return connect.NewResponse(&authv1.CompleteOAuthResponse{Tokens: s.tokens()}), nil
}

func (s *stub) RefreshToken(
	_ context.Context, req *connect.Request[authv1.RefreshTokenRequest],
) (*connect.Response[authv1.RefreshTokenResponse], error) {
	s.seen = req.Msg.GetRefreshToken()
	if s.fail {
		return nil, connect.NewError(connect.CodeUnauthenticated, errStub)
	}
	return connect.NewResponse(&authv1.RefreshTokenResponse{Tokens: s.tokens()}), nil
}

func (s *stub) Logout(
	_ context.Context, req *connect.Request[authv1.LogoutRequest],
) (*connect.Response[authv1.LogoutResponse], error) {
	s.seen = req.Msg.GetRefreshToken()
	if s.fail {
		return nil, connect.NewError(connect.CodeInternal, errStub)
	}
	return connect.NewResponse(&authv1.LogoutResponse{}), nil
}

var errStub = errors.New("the stub was told to fail")

type harness struct {
	stub   *stub
	client authv1connect.AuthServiceClient
}

func newHarness(t *testing.T, cfg cookie.Config) *harness {
	t.Helper()

	h := &harness{stub: &stub{}}

	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(h.stub,
		connect.WithInterceptors(cookie.New(cfg)),
	))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	h.client = authv1connect.NewAuthServiceClient(srv.Client(), srv.URL)
	return h
}

func defaultConfig() cookie.Config {
	return cookie.Config{MaxAge: 720 * time.Hour}
}

func browser[T any](req *connect.Request[T], refreshCookie string) *connect.Request[T] {
	req.Header().Set("Origin", "http://localhost:3000")
	if refreshCookie != "" {
		req.Header().Set("Cookie", (&http.Cookie{
			Name:  cookie.Name,
			Value: refreshCookie,
		}).String())
	}
	return req
}

func refreshCookie(t *testing.T, header http.Header) *http.Cookie {
	t.Helper()

	for _, c := range (&http.Response{Header: header}).Cookies() {
		if c.Name == cookie.Name {
			return c
		}
	}
	return nil
}

func TestBrowserSignInMovesTheTokenIntoACookie(t *testing.T) {
	h := newHarness(t, defaultConfig())

	resp, err := h.client.Login(t.Context(), browser(connect.NewRequest(&authv1.LoginRequest{}), ""))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if got := resp.Msg.GetTokens().GetRefreshToken(); got != "" {
		t.Errorf("the body still carries a refresh token (%q); a script on the page can read it", got)
	}

	c := refreshCookie(t, resp.Header())
	if c == nil {
		t.Fatal("no refresh cookie was set")
	}
	if c.Value != issuedToken {
		t.Errorf("cookie value = %q, want %q", c.Value, issuedToken)
	}
}

func TestEveryIssuingProcedureSetsTheCookie(t *testing.T) {
	calls := map[string]func(*harness) (http.Header, string, error){
		"Register": func(h *harness) (http.Header, string, error) {
			r, err := h.client.Register(t.Context(), browser(connect.NewRequest(&authv1.RegisterRequest{}), ""))
			if err != nil {
				return nil, "", err
			}
			return r.Header(), r.Msg.GetTokens().GetRefreshToken(), nil
		},
		"Login": func(h *harness) (http.Header, string, error) {
			r, err := h.client.Login(t.Context(), browser(connect.NewRequest(&authv1.LoginRequest{}), ""))
			if err != nil {
				return nil, "", err
			}
			return r.Header(), r.Msg.GetTokens().GetRefreshToken(), nil
		},
		"CompleteOAuth": func(h *harness) (http.Header, string, error) {
			r, err := h.client.CompleteOAuth(t.Context(), browser(connect.NewRequest(&authv1.CompleteOAuthRequest{}), ""))
			if err != nil {
				return nil, "", err
			}
			return r.Header(), r.Msg.GetTokens().GetRefreshToken(), nil
		},
		"RefreshToken": func(h *harness) (http.Header, string, error) {
			r, err := h.client.RefreshToken(t.Context(), browser(connect.NewRequest(&authv1.RefreshTokenRequest{}), "old-token"))
			if err != nil {
				return nil, "", err
			}
			return r.Header(), r.Msg.GetTokens().GetRefreshToken(), nil
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, defaultConfig())

			header, body, err := call(h)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if body != "" {
				t.Errorf("%s left the refresh token in the body", name)
			}
			if refreshCookie(t, header) == nil {
				t.Errorf("%s set no refresh cookie", name)
			}
		})
	}
}

func TestNonBrowserClientsKeepTheTokenInTheBody(t *testing.T) {
	h := newHarness(t, defaultConfig())

	resp, err := h.client.Login(t.Context(), connect.NewRequest(&authv1.LoginRequest{}))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if got := resp.Msg.GetTokens().GetRefreshToken(); got != issuedToken {
		t.Errorf("refresh token = %q, want %q", got, issuedToken)
	}
	if c := refreshCookie(t, resp.Header()); c != nil {
		t.Error("a cookie was set for a client that never asked for one")
	}
}

func TestCookieAttributes(t *testing.T) {
	h := newHarness(t, cookie.Config{Secure: true, MaxAge: 48 * time.Hour})

	resp, err := h.client.Login(t.Context(), browser(connect.NewRequest(&authv1.LoginRequest{}), ""))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	c := refreshCookie(t, resp.Header())
	if c == nil {
		t.Fatal("no refresh cookie was set")
	}

	if !c.HttpOnly {
		t.Error("cookie is not HttpOnly, which defeats the entire point")
	}
	if !c.Secure {
		t.Error("Secure was configured on but the cookie does not carry it")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != cookie.Path {
		t.Errorf("Path = %q, want %q — a wider path attaches the credential to unrelated calls", c.Path, cookie.Path)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want host-only", c.Domain)
	}
	if c.MaxAge != int((48 * time.Hour).Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int((48 * time.Hour).Seconds()))
	}
}

func TestSecureFollowsConfiguration(t *testing.T) {
	h := newHarness(t, defaultConfig())

	resp, err := h.client.Login(t.Context(), browser(connect.NewRequest(&authv1.LoginRequest{}), ""))
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if c := refreshCookie(t, resp.Header()); c == nil || c.Secure {
		t.Error("Secure was configured off but the cookie carries it")
	}
}

func TestRefreshReadsTheTokenFromTheCookie(t *testing.T) {
	h := newHarness(t, defaultConfig())

	req := browser(connect.NewRequest(&authv1.RefreshTokenRequest{}), "token-from-the-cookie")
	if _, err := h.client.RefreshToken(t.Context(), req); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if h.stub.seen != "token-from-the-cookie" {
		t.Errorf("auth received %q, want the cookie's value", h.stub.seen)
	}
}

func TestABrowserBodyTokenIsIgnored(t *testing.T) {
	h := newHarness(t, defaultConfig())

	req := browser(connect.NewRequest(&authv1.RefreshTokenRequest{
		RefreshToken: "smuggled-in-the-body",
	}), "")

	if _, err := h.client.RefreshToken(t.Context(), req); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if h.stub.seen != "" {
		t.Errorf("auth received %q; a browser's body must never be trusted for this", h.stub.seen)
	}
}

func TestLogoutClearsTheCookie(t *testing.T) {
	h := newHarness(t, defaultConfig())

	req := browser(connect.NewRequest(&authv1.LogoutRequest{}), "token-being-revoked")
	resp, err := h.client.Logout(t.Context(), req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}

	if h.stub.seen != "token-being-revoked" {
		t.Errorf("auth received %q, want the cookie's value", h.stub.seen)
	}

	c := refreshCookie(t, resp.Header())
	if c == nil {
		t.Fatal("logout set no cookie, so the browser keeps presenting a revoked token")
	}
	if c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("cookie = %q with MaxAge %d, want an expiry", c.Value, c.MaxAge)
	}
}

func TestAFailedCallLeavesTheCookieAlone(t *testing.T) {
	for _, name := range []string{"Login", "Logout"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, defaultConfig())
			h.stub.fail = true

			var header http.Header
			var err error

			switch name {
			case "Login":
				var resp *connect.Response[authv1.LoginResponse]
				resp, err = h.client.Login(t.Context(), browser(connect.NewRequest(&authv1.LoginRequest{}), ""))
				if resp != nil {
					header = resp.Header()
				}
			case "Logout":
				var resp *connect.Response[authv1.LogoutResponse]
				resp, err = h.client.Logout(t.Context(), browser(connect.NewRequest(&authv1.LogoutRequest{}), "still-valid"))
				if resp != nil {
					header = resp.Header()
				}
			}

			if err == nil {
				t.Fatal("the stub was set to fail but the call succeeded")
			}
			if header != nil && refreshCookie(t, header) != nil {
				t.Error("a failed call touched the cookie")
			}
		})
	}
}

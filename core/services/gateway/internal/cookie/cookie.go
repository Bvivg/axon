package cookie

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

const Name = "axon_refresh"

const Path = "/axon.auth.v1.AuthService/"

type Config struct {
	Secure bool

	MaxAge time.Duration
}

type Interceptor struct {
	cfg Config
}

var _ connect.Interceptor = (*Interceptor)(nil)

func New(cfg Config) *Interceptor {
	return &Interceptor{cfg: cfg}
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {

		if req.Spec().IsClient || !isBrowser(req.Header()) {
			return next(ctx, req)
		}

		i.intoRequest(req)

		resp, err := next(ctx, req)
		if err != nil {

			return resp, err
		}

		i.outOfResponse(resp)
		return resp, nil
	}
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func isBrowser(h http.Header) bool {
	return h.Get("Origin") != ""
}

func (i *Interceptor) intoRequest(req connect.AnyRequest) {
	switch msg := req.Any().(type) {
	case *authv1.RefreshTokenRequest:
		msg.RefreshToken = fromCookie(req.Header())
	case *authv1.LogoutRequest:
		msg.RefreshToken = fromCookie(req.Header())
	}
}

func (i *Interceptor) outOfResponse(resp connect.AnyResponse) {
	switch msg := resp.Any().(type) {
	case *authv1.RegisterResponse:
		i.store(resp.Header(), msg.GetTokens())
	case *authv1.LoginResponse:
		i.store(resp.Header(), msg.GetTokens())
	case *authv1.CompleteOAuthResponse:
		i.store(resp.Header(), msg.GetTokens())
	case *authv1.RefreshTokenResponse:
		i.store(resp.Header(), msg.GetTokens())
	case *authv1.LogoutResponse:

		i.clear(resp.Header())
	}
}

func (i *Interceptor) store(h http.Header, tokens *authv1.TokenPair) {
	if tokens.GetRefreshToken() == "" {
		return
	}

	setCookie(h, i.cookie(tokens.GetRefreshToken(), int(i.cfg.MaxAge.Seconds())))
	if tokens.GetAccessToken() != "" {
		setCookie(h, i.mediaCookie("/", "", -1))
		setCookie(h, i.mediaCookie(authn.MediaPath, tokens.GetAccessToken(), int(tokens.GetExpiresIn())))
	}

	tokens.RefreshToken = ""
}

func (i *Interceptor) clear(h http.Header) {

	setCookie(h, i.cookie("", -1))
	setCookie(h, i.mediaCookie("/", "", -1))
	setCookie(h, i.mediaCookie(authn.MediaPath, "", -1))
}

func (i *Interceptor) mediaCookie(path, value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: authn.MediaCookieName, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: i.cfg.Secure, SameSite: http.SameSiteLaxMode}
}

func (i *Interceptor) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     Name,
		Value:    value,
		Path:     Path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   i.cfg.Secure,

		SameSite: http.SameSiteLaxMode,
	}
}

func setCookie(h http.Header, c *http.Cookie) {
	if v := c.String(); v != "" {
		h.Add("Set-Cookie", v)
	}
}

func fromCookie(h http.Header) string {
	c, err := (&http.Request{Header: h}).Cookie(Name)
	if err != nil {
		return ""
	}
	return c.Value
}

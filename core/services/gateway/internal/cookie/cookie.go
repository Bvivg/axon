// Package cookie keeps the refresh token out of the browser's JavaScript.
//
// The contract returns a refresh token in the response body, which is right for
// a native client holding it in the keychain and wrong for a browser: anything
// a page's script can read, an injected script can read too, and a refresh token
// is the long-lived credential of the pair. So for browsers the gateway moves it
// into an HttpOnly cookie on the way out and puts it back on the way in. The
// page never sees it and never has to store it.
//
// Nothing about the contract changes. The field stays where it is and simply
// arrives empty, so a client that does not participate is unaffected.
package cookie

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
)

// Name is the cookie the refresh token lives in.
const Name = "axon_refresh"

// Path scopes the cookie to the auth service's procedures.
//
// It is the Connect procedure prefix, so the browser attaches the cookie to
// sign-in traffic and to nothing else. A credential that rides along on every
// chat and game call for no reason is a credential with a larger blast radius
// than it needs.
const Path = "/axon.auth.v1.AuthService/"

// Config configures the cookie's attributes.
type Config struct {
	// Secure keeps the cookie off plaintext connections. It is on everywhere but
	// local development: browsers accept Secure cookies on http://localhost,
	// which is a trustworthy origin to them, but not over plain HTTP to a LAN
	// address — and that is the only case this switch exists for.
	Secure bool

	// MaxAge is how long the browser keeps the cookie. It should not exceed the
	// refresh token's own lifetime in auth; a longer value only leaves the
	// browser holding a cookie the server already considers dead.
	MaxAge time.Duration
}

// Interceptor translates between the body field and the cookie.
type Interceptor struct {
	cfg Config
}

var _ connect.Interceptor = (*Interceptor)(nil)

// New returns an Interceptor with the given cookie attributes.
func New(cfg Config) *Interceptor {
	return &Interceptor{cfg: cfg}
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		// Client-side calls are the gateway talking to a service. There is no
		// browser on that side of the wire.
		if req.Spec().IsClient || !isBrowser(req.Header()) {
			return next(ctx, req)
		}

		i.intoRequest(req)

		resp, err := next(ctx, req)
		if err != nil {
			// A failed call leaves the cookie exactly as it was. In particular a
			// logout that did not happen must not look like one that did.
			return resp, err
		}

		i.outOfResponse(resp)
		return resp, nil
	}
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler is a pass-through: the contract has no streaming
// procedure that carries a refresh token, and realtime traffic is a separate
// WebSocket protocol rather than a Connect stream.
func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

// isBrowser reports whether the caller is a browser.
//
// The Origin header is the signal. Browsers are required to send it on every
// cross-origin POST, and Connect carries every procedure over POST, so a browser
// cannot suppress it — page script has no way to opt out of the cookie and be
// handed the token in the body instead. Other clients (connect-go, connect-swift)
// do not send it and keep the body behaviour they were written against.
func isBrowser(h http.Header) bool {
	return h.Get("Origin") != ""
}

// intoRequest fills in the refresh token the browser did not send.
//
// The cookie is the only source: whatever is in the body is discarded first.
// Accepting either would mean two ways to present the same credential, and the
// weaker one would be the one an attacker picks.
func (i *Interceptor) intoRequest(req connect.AnyRequest) {
	switch msg := req.Any().(type) {
	case *authv1.RefreshTokenRequest:
		msg.RefreshToken = fromCookie(req.Header())
	case *authv1.LogoutRequest:
		msg.RefreshToken = fromCookie(req.Header())
	}
}

// outOfResponse moves a freshly issued refresh token into the cookie.
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
		// The chain is revoked server-side; the cookie has to go too, or the
		// browser keeps presenting a token that can only ever be refused.
		i.clear(resp.Header())
	}
}

// store writes the token to a cookie and blanks the field it came from.
func (i *Interceptor) store(h http.Header, tokens *authv1.TokenPair) {
	if tokens.GetRefreshToken() == "" {
		return
	}

	setCookie(h, i.cookie(tokens.GetRefreshToken(), int(i.cfg.MaxAge.Seconds())))

	// The whole point. Leaving it in place as well would put the token back
	// within reach of any script on the page.
	tokens.RefreshToken = ""
}

// clear expires the cookie.
func (i *Interceptor) clear(h http.Header) {
	// A negative MaxAge is how a cookie is deleted; zero would mean "session
	// cookie" and leave it in place for the rest of the browsing session.
	setCookie(h, i.cookie("", -1))
}

// cookie builds the cookie with the attributes every write shares.
func (i *Interceptor) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     Name,
		Value:    value,
		Path:     Path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   i.cfg.Secure,
		// Lax is enough in both development and production, because SameSite
		// ignores the port: localhost:3000 and localhost:18080 are one site, as
		// axon.dev and api.axon.dev will be. None would require Secure
		// unconditionally and hand the cookie to any third-party page that
		// managed to make the browser call us.
		SameSite: http.SameSiteLaxMode,
		// Domain is deliberately unset, which makes the cookie host-only: a
		// sibling subdomain cannot read it or overwrite it.
	}
}

// setCookie appends a Set-Cookie header. http.SetCookie needs a ResponseWriter,
// and an interceptor only has the header map.
func setCookie(h http.Header, c *http.Cookie) {
	if v := c.String(); v != "" {
		h.Add("Set-Cookie", v)
	}
}

// fromCookie reads the token the browser sent back, or "" if it sent none.
func fromCookie(h http.Header) string {
	c, err := (&http.Request{Header: h}).Cookie(Name)
	if err != nil {
		return ""
	}
	return c.Value
}

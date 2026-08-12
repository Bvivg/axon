package wsproxy_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/gateway/internal/wsproxy"
)

const (
	testKeyID     = "gateway-test-key"
	testIssuer    = "https://auth.axon.test"
	testAudience  = "axon"
	testProtocol  = "axon.chat.v1"
	testOrigin    = "http://web.axon.test"
	readTimeout   = 5 * time.Second
	upstreamClose = ws.StatusCode(4402)
)

// upstream stands in for the chat service: it records what reached it and
// answers however a scenario needs.
type upstreamService struct {
	server *httptest.Server

	// authorization is the header the last connection arrived with.
	authorization chan string

	// closeWith, when set, makes the upstream hang up with that code instead of
	// echoing.
	closeWith ws.StatusCode
}

func newUpstream(t *testing.T) *upstreamService {
	t.Helper()

	u := &upstreamService{authorization: make(chan string, 4)}

	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.authorization <- r.Header.Get("Authorization")

		conn, err := ws.Accept(w, r, ws.AcceptOptions{
			Subprotocols: []string{testProtocol},
			Options:      ws.Options{Logger: logger.Discard()},
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		if u.closeWith != 0 {
			_ = conn.Close(u.closeWith, "access token expired")
			return
		}

		// Echo, so a test can watch a frame make the round trip.
		for {
			payload, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), append([]byte("echo:"), payload...)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(u.server.Close)

	return u
}

func (u *upstreamService) socketURL() string {
	return "ws" + strings.TrimPrefix(u.server.URL, "http")
}

// harness is a gateway proxy in front of an upstream.
type harness struct {
	proxy    *httptest.Server
	upstream *upstreamService
	sign     func(t *testing.T, ttl time.Duration) string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     staticKeys{id: testKeyID, key: &key.PublicKey},
		Issuer:   testIssuer,
		Audience: testAudience,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	upstream := newUpstream(t)

	handler, err := wsproxy.New(wsproxy.Config{
		Upstream: upstream.socketURL(),
		Protocol: testProtocol,
		Verifier: verifier,
		Origins:  []string{testOrigin},
		Logger:   logger.Discard(),
	})
	if err != nil {
		t.Fatalf("wsproxy.New: %v", err)
	}

	proxy := httptest.NewServer(handler)
	t.Cleanup(proxy.Close)

	return &harness{
		proxy:    proxy,
		upstream: upstream,
		sign: func(t *testing.T, ttl time.Duration) string {
			t.Helper()
			return signToken(t, key, ttl)
		},
	}
}

// dial connects the way a browser does: the token as a subprotocol offer, since
// the WebSocket API gives a page no way to set a header.
func (h *harness) dial(t *testing.T, token string) (*ws.Conn, error) {
	t.Helper()

	protocols := []string{testProtocol}
	if token != "" {
		protocols = append(protocols, wsproxy.BearerPrefix+token)
	}

	header := http.Header{}
	header.Set("Origin", testOrigin)

	return ws.Dial(t.Context(), "ws"+strings.TrimPrefix(h.proxy.URL, "http"), ws.DialOptions{
		Subprotocols: protocols,
		Header:       header,
		Options:      ws.Options{Logger: logger.Discard()},
	})
}

// A page cannot set an Authorization header on an upgrade, so the token arrives
// as a subprotocol offer — and the gateway turns it into a real header on the
// hop to the service, which verifies it itself.
func TestTheTokenBecomesAnAuthorizationHeaderUpstream(t *testing.T) {
	h := newHarness(t)

	token := h.sign(t, time.Hour)

	conn, err := h.dial(t, token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	select {
	case got := <-h.upstream.authorization:
		if got != "Bearer "+token {
			t.Errorf("upstream received %q, want the caller's own bearer token", got)
		}
	case <-time.After(readTimeout):
		t.Fatal("the upstream was never reached")
	}

	// Only the conversation protocol is negotiated back. The bearer offer was a
	// way to carry a credential, not something either end speaks.
	if got := conn.Subprotocol(); got != testProtocol {
		t.Errorf("negotiated %q, want %q", got, testProtocol)
	}
}

func TestAnUpgradeWithoutATokenIsRefused(t *testing.T) {
	h := newHarness(t)

	if _, err := h.dial(t, ""); err == nil {
		t.Fatal("a socket opened with no token")
	}
}

func TestAnUpgradeWithAForgedTokenIsRefused(t *testing.T) {
	h := newHarness(t)

	if _, err := h.dial(t, "not-a-token"); err == nil {
		t.Fatal("a socket opened with a forged token")
	}
}

// An expired token is refused at the gateway, before anything internal is
// dialled at all.
func TestAnExpiredTokenNeverReachesTheService(t *testing.T) {
	h := newHarness(t)

	if _, err := h.dial(t, h.sign(t, -time.Minute)); err == nil {
		t.Fatal("a socket opened with an expired token")
	}

	select {
	case got := <-h.upstream.authorization:
		t.Fatalf("the upstream was reached with %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// The upgrade carries credentials, so it is held to the same origin allow-list
// as every other credentialed request.
func TestAnUpgradeFromAnUnlistedOriginIsRefused(t *testing.T) {
	h := newHarness(t)

	header := http.Header{}
	header.Set("Origin", "http://evil.example")

	_, err := ws.Dial(t.Context(), "ws"+strings.TrimPrefix(h.proxy.URL, "http"), ws.DialOptions{
		Subprotocols: []string{testProtocol, wsproxy.BearerPrefix + h.sign(t, time.Hour)},
		Header:       header,
		Options:      ws.Options{Logger: logger.Discard()},
	})
	if err == nil {
		t.Fatal("a socket opened from an origin that is not on the list")
	}
}

// Frames cross untouched in both directions. The gateway does not know what a
// chat frame means and must not learn: the protocol belongs to the service.
func TestFramesCrossInBothDirections(t *testing.T) {
	h := newHarness(t)

	conn, err := h.dial(t, h.sign(t, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithTimeout(t.Context(), readTimeout)
	defer cancel()

	if err := conn.Write(ctx, []byte(`{"type":"anything"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := `echo:{"type":"anything"}`; string(payload) != want {
		t.Errorf("received %q, want %q", payload, want)
	}
}

// The service's own close codes reach the browser as themselves. A client told
// "the connection ended" rather than "your token expired" has no idea whether
// reconnecting will help.
func TestTheServicesCloseCodeReachesTheBrowser(t *testing.T) {
	h := newHarness(t)
	h.upstream.closeWith = upstreamClose

	conn, err := h.dial(t, h.sign(t, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	ctx, cancel := context.WithTimeout(t.Context(), readTimeout)
	defer cancel()

	_, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("the socket stayed open after the service closed it")
	}
	if got := ws.CloseStatus(err); got != upstreamClose {
		t.Errorf("closed with %d, want %d", got, upstreamClose)
	}
}

// When the browser goes away the upstream connection goes with it, rather than
// waiting for a keepalive to notice.
func TestClosingTheBrowserSideReleasesTheUpstream(t *testing.T) {
	h := newHarness(t)

	conn, err := h.dial(t, h.sign(t, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), readTimeout)
	defer cancel()

	// One round trip, so both directions are certainly running before the
	// connection is taken away.
	if err := conn.Write(ctx, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}

	if err := conn.Close(ws.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	// The upstream handler returns when its read fails, which releases the
	// server's handler goroutine. Observed through the test server's own
	// shutdown: Close blocks until outstanding handlers return, so a proxy that
	// leaked the upstream connection would hang here.
	done := make(chan struct{})
	go func() {
		h.upstream.server.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(readTimeout):
		t.Fatal("the upstream connection outlived the browser's")
	}
}

// staticKeys is the key set the tests sign against.
type staticKeys struct {
	id  string
	key *rsa.PublicKey
}

func (s staticKeys) PublicKey(keyID string) (*rsa.PublicKey, bool) {
	if keyID != s.id {
		return nil, false
	}
	return s.key, true
}

func signToken(t *testing.T, key *rsa.PrivateKey, ttl time.Duration) string {
	t.Helper()

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": uuid.NewString(),
		"iss": testIssuer,
		"aud": testAudience,
		"jti": uuid.NewString(),
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	token.Header["kid"] = testKeyID

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

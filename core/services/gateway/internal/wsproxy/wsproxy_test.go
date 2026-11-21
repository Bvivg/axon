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

	"github.com/bvivg/axon/core/services/gateway/internal/ratelimit"
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

type upstreamService struct {
	server *httptest.Server

	authorization chan string

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

type harness struct {
	proxy    *httptest.Server
	upstream *upstreamService
	sign     func(t *testing.T, ttl time.Duration) string
	signFor  func(t *testing.T, user uuid.UUID) string
}

type limits struct {
	upgrades   int
	maxPerUser int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newLimitedHarness(t, limits{})
}

func newLimitedHarness(t *testing.T, l limits) *harness {
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

	var limiter *ratelimit.Limiter
	if l.upgrades > 0 {
		limiter = ratelimit.New(ratelimit.Config{PerMinute: l.upgrades, Burst: l.upgrades})
	}

	handler, err := wsproxy.New(wsproxy.Config{
		Upstream:   upstream.socketURL(),
		Protocol:   testProtocol,
		Verifier:   verifier,
		Origins:    []string{testOrigin},
		Logger:     logger.Discard(),
		Limiter:    limiter,
		MaxPerUser: l.maxPerUser,
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
			return signToken(t, key, uuid.New(), ttl)
		},
		signFor: func(t *testing.T, user uuid.UUID) string {
			t.Helper()
			return signToken(t, key, user, time.Hour)
		},
	}
}

func (h *harness) upgradeStatus(t *testing.T, token string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, h.proxy.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("Sec-WebSocket-Protocol", testProtocol+", "+wsproxy.BearerPrefix+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

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

func TestClosingTheBrowserSideReleasesTheUpstream(t *testing.T) {
	h := newHarness(t)

	conn, err := h.dial(t, h.sign(t, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), readTimeout)
	defer cancel()

	if err := conn.Write(ctx, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read: %v", err)
	}

	if err := conn.Close(ws.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

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

func signToken(t *testing.T, key *rsa.PrivateKey, user uuid.UUID, ttl time.Duration) string {
	t.Helper()

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": user.String(),
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

func TestUpgradesArePacedPerUser(t *testing.T) {
	h := newLimitedHarness(t, limits{upgrades: 2})
	user := uuid.New()

	for i := range 2 {
		if status := h.upgradeStatus(t, h.signFor(t, user)); status == http.StatusTooManyRequests {
			t.Fatalf("upgrade %d inside the budget was refused", i+1)
		}
	}
	if status := h.upgradeStatus(t, h.signFor(t, user)); status != http.StatusTooManyRequests {
		t.Fatalf("a third upgrade inside the window: got %d, want 429", status)
	}
	if status := h.upgradeStatus(t, h.signFor(t, uuid.New())); status == http.StatusTooManyRequests {
		t.Fatal("another user was throttled by the first one's upgrades")
	}
}

func TestFailedUpgradesSpendTheAddressBudget(t *testing.T) {
	h := newLimitedHarness(t, limits{upgrades: 2})

	for range 2 {
		if status := h.upgradeStatus(t, "forged"); status != http.StatusUnauthorized {
			t.Fatalf("a forged token: got %d, want 401", status)
		}
	}
	if status := h.upgradeStatus(t, "forged"); status != http.StatusTooManyRequests {
		t.Fatalf("token guessing past the budget: got %d, want 429", status)
	}
}

func TestOpenSocketsPerUserAreCapped(t *testing.T) {
	h := newLimitedHarness(t, limits{maxPerUser: 2})
	user := uuid.New()

	var open []*ws.Conn
	for i := range 2 {
		conn, err := h.dial(t, h.signFor(t, user))
		if err != nil {
			t.Fatalf("socket %d inside the cap was refused: %v", i+1, err)
		}
		open = append(open, conn)
	}
	if conn, err := h.dial(t, h.signFor(t, user)); err == nil {
		_ = conn.CloseNow()
		t.Fatal("a third concurrent socket was accepted")
	}
	if conn, err := h.dial(t, h.signFor(t, uuid.New())); err != nil {
		t.Fatalf("another user was refused: %v", err)
	} else {
		_ = conn.CloseNow()
	}

	_ = open[0].Close(ws.StatusNormalClosure, "done")
	deadline := time.Now().Add(readTimeout)
	for {
		conn, err := h.dial(t, h.signFor(t, user))
		if err == nil {
			_ = conn.CloseNow()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closing a socket never freed its slot")
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = open[1].CloseNow()
}

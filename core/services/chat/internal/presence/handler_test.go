package presence_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/chat/internal/presence"
)

type touch struct {
	sessionID string
	ttl       time.Duration
}

type fakeTracker struct {
	mu       sync.Mutex
	touches  []touch
	cleared  []string
	touchedC chan struct{}
	clearedC chan struct{}
}

func newFakeTracker() *fakeTracker {
	return &fakeTracker{
		touchedC: make(chan struct{}, 16),
		clearedC: make(chan struct{}, 16),
	}
}

func (f *fakeTracker) Touch(_ context.Context, sessionID, _ string, ttl time.Duration) error {
	f.mu.Lock()
	f.touches = append(f.touches, touch{sessionID: sessionID, ttl: ttl})
	f.mu.Unlock()
	f.touchedC <- struct{}{}
	return nil
}

func (f *fakeTracker) Clear(_ context.Context, sessionID, _ string) error {
	f.mu.Lock()
	f.cleared = append(f.cleared, sessionID)
	f.mu.Unlock()
	f.clearedC <- struct{}{}
	return nil
}

func (f *fakeTracker) PublishChanged(context.Context, string) error {
	return nil
}

type fakeLastSeen struct {
	mu       sync.Mutex
	recorded []string
	seenC    chan struct{}
}

func newFakeLastSeen() *fakeLastSeen {
	return &fakeLastSeen{seenC: make(chan struct{}, 16)}
}

func (f *fakeLastSeen) UpdateLastSeen(_ context.Context, accessToken string) {
	f.mu.Lock()
	f.recorded = append(f.recorded, accessToken)
	f.mu.Unlock()
	f.seenC <- struct{}{}
}

func (f *fakeLastSeen) waitRecorded(t *testing.T) {
	t.Helper()
	select {
	case <-f.seenC:
	case <-time.After(5 * time.Second):
		t.Fatal("last seen was never recorded")
	}
}

func (f *fakeTracker) waitTouched(t *testing.T) {
	t.Helper()
	select {
	case <-f.touchedC:
	case <-time.After(5 * time.Second):
		t.Fatal("presence was never touched")
	}
}

func (f *fakeTracker) waitCleared(t *testing.T) {
	t.Helper()
	select {
	case <-f.clearedC:
	case <-time.After(5 * time.Second):
		t.Fatal("presence was never cleared")
	}
}

func (f *fakeTracker) sessionIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, len(f.touches))
	for i, tc := range f.touches {
		ids[i] = tc.sessionID
	}
	return ids
}

type harness struct {
	server   *httptest.Server
	tracker  *fakeTracker
	lastSeen *fakeLastSeen
	issue    func(t *testing.T, userID, familyID uuid.UUID, ttl time.Duration) string
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	const (
		keyID    = "test-key-1"
		issuer   = "https://auth.axon.test"
		audience = "axon"
	)

	verifier, err := authn.NewVerifier(authn.Config{
		Keys:     staticKeys{id: keyID, key: &key.PublicKey},
		Issuer:   issuer,
		Audience: audience,
	})
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}

	tracker := newFakeTracker()
	lastSeen := newFakeLastSeen()

	handler, err := presence.New(presence.Config{
		Verifier: verifier,
		Tracker:  tracker,
		LastSeen: lastSeen,
		Logger:   logger.Discard(),
	})
	if err != nil {
		t.Fatalf("presence.New: %v", err)
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &harness{
		server:   server,
		tracker:  tracker,
		lastSeen: lastSeen,
		issue: func(t *testing.T, userID, familyID uuid.UUID, ttl time.Duration) string {
			t.Helper()
			return signToken(t, key, keyID, issuer, audience, userID, familyID, ttl)
		},
	}
}

func (h *harness) dial(t *testing.T, token string) (*sharedws.Conn, error) {
	t.Helper()

	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	return sharedws.Dial(t.Context(), wsURL(h.server.URL), sharedws.DialOptions{
		Subprotocols: []string{presence.Subprotocol},
		Header:       header,
	})
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + presence.Path
}

func TestConnectingTouchesPresenceForTheSession(t *testing.T) {
	h := newHarness(t)

	userID, familyID := uuid.New(), uuid.New()
	conn, err := h.dial(t, h.issue(t, userID, familyID, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	h.tracker.waitTouched(t)

	ids := h.tracker.sessionIDs()
	if len(ids) == 0 || ids[0] != familyID.String() {
		t.Errorf("touched sessions = %v, want to include %q", ids, familyID.String())
	}
}

func TestDisconnectingClearsPresence(t *testing.T) {
	h := newHarness(t)

	userID, familyID := uuid.New(), uuid.New()
	conn, err := h.dial(t, h.issue(t, userID, familyID, time.Hour))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	h.tracker.waitTouched(t)

	if err := conn.Close(sharedws.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	h.tracker.waitCleared(t)
	h.lastSeen.waitRecorded(t)

	h.tracker.mu.Lock()
	cleared := append([]string(nil), h.tracker.cleared...)
	h.tracker.mu.Unlock()

	if len(cleared) == 0 || cleared[0] != familyID.String() {
		t.Errorf("cleared sessions = %v, want to include %q", cleared, familyID.String())
	}
}

func TestAnExpiringTokenRotatesTheSocketWithoutGoingOffline(t *testing.T) {
	h := newHarness(t)

	userID, familyID := uuid.New(), uuid.New()
	conn, err := h.dial(t, h.issue(t, userID, familyID, 5*time.Second+500*time.Millisecond))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })

	h.tracker.waitTouched(t)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if _, err := conn.Read(ctx); err == nil {
		t.Fatal("the socket stayed open past the token's expiry")
	} else if code := sharedws.CloseStatus(err); code != presence.CloseTokenExpired {
		t.Errorf("closed with %d, want %d", code, presence.CloseTokenExpired)
	}

	h.lastSeen.waitRecorded(t)
	time.Sleep(200 * time.Millisecond)

	h.tracker.mu.Lock()
	cleared := len(h.tracker.cleared)
	h.tracker.mu.Unlock()

	if cleared != 0 {
		t.Errorf("presence was cleared %d times on a token rotation, want 0", cleared)
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

func signToken(
	t *testing.T,
	key *rsa.PrivateKey,
	keyID, issuer, audience string,
	userID, familyID uuid.UUID,
	ttl time.Duration,
) string {
	t.Helper()

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": userID.String(),
		"iss": issuer,
		"aud": audience,
		"jti": uuid.NewString(),
		"fid": familyID.String(),
		"iat": now.Unix(),
		"exp": now.Add(ttl).Unix(),
	})
	token.Header["kid"] = keyID

	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

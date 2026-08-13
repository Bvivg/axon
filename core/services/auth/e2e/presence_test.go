//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
	"time"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"
)

const (
	presenceSubprotocol  = "axon.presence.v1"
	presenceBearerPrefix = "axon.bearer."
	presenceOrigin       = "http://localhost:3000"
)

func presenceSocketURL() string {
	return "ws" + strings.TrimPrefix(gatewayURL, "http") + "/ws/presence"
}

func dialPresence(t *testing.T, accessToken string) *sharedws.Conn {
	t.Helper()

	header := http.Header{}
	header.Set("Origin", presenceOrigin)

	conn, err := sharedws.Dial(t.Context(), presenceSocketURL(), sharedws.DialOptions{
		Subprotocols: []string{presenceSubprotocol, presenceBearerPrefix + accessToken},
		Header:       header,
	})
	if err != nil {
		t.Fatalf("dial presence socket: %v", err)
	}
	return conn
}

func waitForOnline(t *testing.T, c *caller, accessToken string, want bool) []*authv1.Session {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	var sessions []*authv1.Session

	for time.Now().Before(deadline) {
		var err error
		sessions, err = c.listSessions(accessToken)
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 1 && sessions[0].GetOnline() == want {
			return sessions
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("session never reported online=%v: %+v", want, sessions)
	return nil
}

func TestPresenceSocketMarksTheSessionOnline(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	conn := dialPresence(t, acct.tokens.GetAccessToken())
	defer func() { _ = conn.CloseNow() }()

	waitForOnline(t, c, acct.tokens.GetAccessToken(), true)
}

func TestPresenceSocketClearsOnDisconnect(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	conn := dialPresence(t, acct.tokens.GetAccessToken())

	waitForOnline(t, c, acct.tokens.GetAccessToken(), true)

	if err := conn.Close(sharedws.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}

	waitForOnline(t, c, acct.tokens.GetAccessToken(), false)
}

func TestPresenceIsScopedToItsOwnSession(t *testing.T) {
	c := newCaller(t)
	acct := c.register(t)

	if _, err := c.login(acct.email, testPassword); err != nil {
		t.Fatalf("second login: %v", err)
	}

	conn := dialPresence(t, acct.tokens.GetAccessToken())
	defer func() { _ = conn.CloseNow() }()

	deadline := time.Now().Add(10 * time.Second)
	var sessions []*authv1.Session
	for time.Now().Before(deadline) {
		var err error
		sessions, err = c.listSessions(acct.tokens.GetAccessToken())
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(sessions) == 2 && currentSession(sessions).GetOnline() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}

	current := currentSession(sessions)
	if current == nil || !current.GetOnline() {
		t.Fatalf("the session with the open presence socket was not marked online: %+v", sessions)
	}

	for _, s := range sessions {
		if s.GetId() != current.GetId() && s.GetOnline() {
			t.Errorf("session %s has no open presence socket but was marked online", s.GetId())
		}
	}
}

func currentSession(sessions []*authv1.Session) *authv1.Session {
	for _, s := range sessions {
		if s.GetCurrent() {
			return s
		}
	}
	return nil
}

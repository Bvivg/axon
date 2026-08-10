package ws_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

// These run against a real connection over loopback rather than a mock. A
// WebSocket wrapper that is only tested against a fake proves nothing about
// pings, close frames or read limits, which are the parts worth having.

// echoServer sends every message straight back and closes with whatever code
// the failure maps to, which is how a real endpoint is meant to behave.
func echoServer(t *testing.T, opts ws.AcceptOptions) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, opts)
		if err != nil {
			return
		}

		// Bounded so a test that forgets to disconnect fails on its own
		// deadline instead of hanging the server's shutdown.
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		for {
			data, err := conn.Read(ctx)
			if err != nil {
				code, reason := ws.CloseCodeFor(err)
				_ = conn.Close(code, reason)
				return
			}
			if err := conn.Write(ctx, data); err != nil {
				_ = conn.CloseNow()
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestConnRoundTrip(t *testing.T) {
	srv := echoServer(t, ws.AcceptOptions{})

	conn, err := ws.Dial(t.Context(), srv.URL, ws.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close(ws.StatusNormalClosure, "done") }()

	if err := conn.Write(t.Context(), []byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := conn.Read(t.Context())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("read %q, want hello", got)
	}
}

// The peer has to learn why it was disconnected, and an over-long reason must
// not cost it the close frame entirely.
func TestCloseDeliversCodeAndTruncatedReason(t *testing.T) {
	reason := strings.Repeat("ю", 100) // 200 bytes, well past what a close frame holds

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, ws.AcceptOptions{})
		if err != nil {
			return
		}
		_ = conn.Close(ws.StatusPolicyViolation, reason)
	}))
	defer srv.Close()

	conn, err := ws.Dial(t.Context(), srv.URL, ws.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	_, err = conn.Read(t.Context())

	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read error = %v, want a close error", err)
	}
	if closeErr.Code != ws.StatusPolicyViolation {
		t.Fatalf("close code = %v, want %v", closeErr.Code, ws.StatusPolicyViolation)
	}
	if len(closeErr.Reason) > ws.MaxCloseReason {
		t.Fatalf("reason is %d bytes, which cannot fit in a close frame", len(closeErr.Reason))
	}
	if !strings.HasPrefix(reason, closeErr.Reason) {
		t.Fatalf("reason %q is not a prefix of what was sent", closeErr.Reason)
	}
}

func TestReadLimitDisconnectsWithMessageTooBig(t *testing.T) {
	srv := echoServer(t, ws.AcceptOptions{Options: ws.Options{ReadLimit: 16}})

	conn, err := ws.Dial(t.Context(), srv.URL, ws.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if err := conn.Write(t.Context(), []byte(strings.Repeat("x", 1024))); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = conn.Read(t.Context())
	if got := websocket.CloseStatus(err); got != ws.StatusMessageTooBig {
		t.Fatalf("close status = %v (err %v), want %v", got, err, ws.StatusMessageTooBig)
	}
}

// Keepalive is the reason a connection to a peer that vanished does not sit
// there holding a session, so the pings have to actually go out.
func TestKeepalivePingsThePeer(t *testing.T) {
	var pings atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The raw library here, not our Accept: counting pings is the point of
		// the test and nothing in the wrapper exposes them.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OnPingReceived: func(_ context.Context, _ []byte) bool {
				pings.Add(1)
				return true
			},
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	conn, err := ws.Dial(t.Context(), srv.URL, ws.DialOptions{Options: ws.Options{
		PingInterval: 20 * time.Millisecond,
		PingTimeout:  2 * time.Second,
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Pongs are processed by the read machinery, so somebody has to be reading
	// — the constraint the package documents.
	dropped := make(chan error, 1)
	go func() {
		_, err := conn.Read(context.Background())
		dropped <- err
	}()

	deadline := time.After(5 * time.Second)
	for pings.Load() < 3 {
		select {
		case err := <-dropped:
			t.Fatalf("connection dropped after %d pings: %v", pings.Load(), err)
		case <-deadline:
			t.Fatalf("saw %d pings in 5s, want at least 3", pings.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}

	if err := conn.Close(ws.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}
	<-dropped
}

// A WebSocket session outlives the request that opened it, so the correlation
// ID has to survive the handshake or the session's logs float free.
func TestCorrelationIDTravelsWithTheHandshake(t *testing.T) {
	serverSide := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ws.Accept(w, r, ws.AcceptOptions{})
		if err != nil {
			serverSide <- ""
			return
		}
		serverSide <- conn.CorrelationID()
		_ = conn.Close(ws.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	ctx := correlation.WithID(t.Context(), "trace-42")

	conn, err := ws.Dial(ctx, srv.URL, ws.DialOptions{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if got := <-serverSide; got != "trace-42" {
		t.Fatalf("server side correlation id = %q, want trace-42", got)
	}
	if got := conn.CorrelationID(); got != "trace-42" {
		t.Fatalf("client side correlation id = %q, want trace-42", got)
	}
}

func TestDialRetryConnectsAfterFailures(t *testing.T) {
	var attempts atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := ws.Accept(w, r, ws.AcceptOptions{})
		if err != nil {
			return
		}
		_ = conn.Close(ws.StatusNormalClosure, "done")
	}))
	defer srv.Close()

	backoff := ws.Backoff{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond, Factor: 2}

	start := time.Now()
	conn, err := ws.DialRetry(t.Context(), srv.URL, ws.DialOptions{}, backoff)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("DialRetry: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	if got := attempts.Load(); got != 3 {
		t.Fatalf("server saw %d attempts, want 3", got)
	}
	// 5ms after the first failure plus 10ms after the second.
	if elapsed < 15*time.Millisecond {
		t.Fatalf("connected in %v, too fast to have waited out the backoff", elapsed)
	}
}

func TestDialRetryStopsWithTheContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "never ready", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := ws.DialRetry(ctx, srv.URL, ws.DialOptions{}, ws.Backoff{
		Initial: 10 * time.Millisecond,
		Max:     10 * time.Millisecond,
		Factor:  1,
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context deadline", err)
	}
}

func TestDialRetryRejectsAnUnusableBackoff(t *testing.T) {
	_, err := ws.DialRetry(t.Context(), "ws://127.0.0.1:1", ws.DialOptions{}, ws.Backoff{})

	if !errors.Is(err, ws.ErrInvalidBackoff) {
		t.Fatalf("err = %v, want ErrInvalidBackoff", err)
	}
}

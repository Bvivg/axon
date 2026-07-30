package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/health"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

func ok(name string) health.Checker {
	return health.CheckerFunc{
		CheckerName: name,
		Fn:          func(context.Context) error { return nil },
	}
}

func failing(name string, err error) health.Checker {
	return health.CheckerFunc{
		CheckerName: name,
		Fn:          func(context.Context) error { return err },
	}
}

// probe runs one request against handler and returns the status and body.
func probe(t *testing.T, h *health.Handler, path string) (int, map[string]any) {
	t.Helper()

	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON (%v): %s", err, rec.Body.String())
	}
	return rec.Code, body
}

// Liveness must stay green while a dependency is down, otherwise a database
// blip restarts every replica.
func TestLiveIgnoresFailingDependencies(t *testing.T) {
	h := health.New(logger.Discard(), []health.Checker{
		failing("postgres", errors.New("connection refused")),
	})

	code, body := probe(t, h, health.PathLive)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", body["status"])
	}
	if _, present := body["checks"]; present {
		t.Errorf("liveness reported dependency checks: %v", body)
	}
}

func TestReadyWithAllCheckersPassing(t *testing.T) {
	h := health.New(logger.Discard(), []health.Checker{ok("postgres"), ok("redis")})

	code, body := probe(t, h, health.PathReady)

	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["postgres"] != "ok" || checks["redis"] != "ok" {
		t.Fatalf("checks = %v, want both ok", checks)
	}
}

func TestReadyWithNoCheckers(t *testing.T) {
	h := health.New(logger.Discard(), nil)

	if code, _ := probe(t, h, health.PathReady); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
}

func TestReadyFailsWhenADependencyIsDown(t *testing.T) {
	h := health.New(logger.Discard(), []health.Checker{
		ok("postgres"),
		failing("redis", errors.New("connection refused")),
	})

	code, body := probe(t, h, health.PathReady)

	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", code)
	}
	if body["status"] != "unavailable" {
		t.Fatalf("status field = %v, want unavailable", body["status"])
	}
	checks, _ := body["checks"].(map[string]any)
	if checks["postgres"] != "ok" {
		t.Errorf("healthy dependency not reported ok: %v", checks)
	}
	// The failing dependency must be named, so a 503 is diagnosable.
	if checks["redis"] != "connection refused" {
		t.Errorf("failure reason for redis = %v, want the error text", checks["redis"])
	}
}

// A dependency that hangs has to fail the probe rather than hang it.
func TestReadyTimesOutOnHangingChecker(t *testing.T) {
	hanging := health.CheckerFunc{
		CheckerName: "postgres",
		Fn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	h := health.New(logger.Discard(), []health.Checker{hanging}, health.WithTimeout(20*time.Millisecond))

	done := make(chan struct{})
	var code int
	go func() {
		defer close(done)
		code, _ = probe(t, h, health.PathReady)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readiness probe did not return; the timeout is not enforced")
	}

	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", code)
	}
}

func TestProbesAreNotCacheable(t *testing.T) {
	h := health.New(logger.Discard(), nil)
	mux := http.NewServeMux()
	h.Register(mux)

	for _, path := range []string{health.PathLive, health.PathReady} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want no-store", path, got)
		}
	}
}

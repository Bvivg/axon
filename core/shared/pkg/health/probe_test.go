package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/health"
)

func TestProbeAcceptsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := health.Probe(t.Context(), srv.URL); err != nil {
		t.Fatalf("a healthy endpoint was reported unhealthy: %v", err)
	}
}

// The probe decides on the status code alone. A readiness handler that reports
// a failing dependency answers 503 with a perfectly well-formed body, and a
// probe that only checked for a response would call that healthy.
func TestProbeRejectsNonSuccess(t *testing.T) {
	for _, code := range []int{
		http.StatusServiceUnavailable,
		http.StatusInternalServerError,
		http.StatusNotFound,
		http.StatusFound,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))

		err := health.Probe(t.Context(), srv.URL)
		srv.Close()

		if err == nil {
			t.Errorf("status %d was reported healthy", code)
		}
	}
}

// The container is starting, or already gone. Either way the probe must fail
// rather than hang.
func TestProbeFailsWhenNothingIsListening(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := health.Probe(t.Context(), url); err == nil {
		t.Fatal("a closed listener was reported healthy")
	}
}

func TestProbeRespectsCancellation(t *testing.T) {
	// A handler that blocks until the client gives up.
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-blocked
	}))
	defer func() {
		close(blocked)
		srv.Close()
	}()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := health.Probe(ctx, srv.URL); err == nil {
		t.Fatal("a cancelled probe reported success")
	}
}

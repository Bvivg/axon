//go:build e2e

// Package e2e drives the running stack the way a real client does: through the
// gateway, over the published contract, with no access to Postgres and no
// knowledge of any service's internals. If a test here can only be made to pass
// by reaching around the gateway, the thing it is testing is not what a client
// experiences.
//
// It never runs on a bare host. See core/deploy/docker-compose.e2e.yml:
//
//	make test-e2e
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/health"
)

var (
	// gatewayURL is the public listener — the only address a client ever has.
	gatewayURL string

	// adminURL is the internal listener carrying probes and metrics. The tests
	// use it to wait for the stack and to assert that what lives here is not
	// also served publicly.
	adminURL string
)

// readyTimeout bounds the wait for the stack. Compose already gates the runner
// on the gateway reporting healthy, so this is a safety net rather than the
// primary mechanism, and a short one: if the stack is not up by now it is not
// coming up.
const readyTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("GATEWAY_URL")
	adminURL = os.Getenv("GATEWAY_ADMIN_URL")

	if gatewayURL == "" || adminURL == "" {
		// Not a failure. `go test ./...` on a developer's machine should not
		// look broken because the stack happens not to be running.
		fmt.Fprintln(os.Stderr, "GATEWAY_URL is not set; skipping e2e. Run: make test-e2e")
		os.Exit(0)
	}

	if err := waitForReady(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// waitForReady blocks until the gateway reports itself ready, which includes
// having fetched auth's key set — without it every authenticated call would
// fail for a reason that has nothing to do with the test.
func waitForReady() error {
	deadline := time.Now().Add(readyTimeout)
	url := adminURL + health.PathReady

	var last error
	for time.Now().Before(deadline) {
		if last = health.Probe(context.Background(), url); last == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("gateway at %s never became ready: %w", adminURL, last)
}

// Probes and metrics belong to whoever operates the service, not to whoever can
// reach it. They are served on a separate listener; this proves the separation
// is real rather than merely intended.
func TestAdminSurfaceIsNotPublic(t *testing.T) {
	for _, path := range []string{health.PathLive, health.PathReady, "/metrics"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, gatewayURL+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s is served on the public listener: status %d", path, resp.StatusCode)
			}
		})
	}
}

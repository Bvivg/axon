//go:build e2e

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
	gatewayURL string

	adminURL string
)

const readyTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("GATEWAY_URL")
	adminURL = os.Getenv("GATEWAY_ADMIN_URL")

	if gatewayURL == "" || adminURL == "" {

		fmt.Fprintln(os.Stderr, "GATEWAY_URL is not set; skipping e2e. Run: make test-e2e")
		os.Exit(0)
	}

	if err := waitForReady(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

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

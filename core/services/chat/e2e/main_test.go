//go:build e2e

// Package e2e drives the running stack the way a real client does: through the
// gateway, over the published contract and over the socket the gateway
// terminates, with no access to Postgres, Redis or any service's internals.
//
// What this suite is for is the half of chat that no unit or integration test
// can reach: a message leaving one client's socket, crossing the gateway, being
// written, fanned out through Redis and arriving at another client's socket —
// with every hop being the real one.
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

	// gatewaySocketURL is where the browser's chat socket is terminated.
	gatewaySocketURL string

	// adminURL is the gateway's internal listener. The probes live there and
	// nowhere else — the public listener answering /healthz would be a finding,
	// and the auth suite asserts that it does not.
	adminURL string
)

// readyTimeout bounds the wait for the stack. Compose already gates the runner
// on the gateway reporting healthy, so this is a safety net.
const readyTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {
		// Not a failure. `go test ./...` on a developer's machine should not
		// look broken because the stack happens not to be running.
		fmt.Fprintln(os.Stderr, "GATEWAY_URL is not set; skipping e2e. Run: make test-e2e")
		os.Exit(0)
	}

	adminURL = os.Getenv("GATEWAY_ADMIN_URL")
	if adminURL == "" {
		fmt.Fprintln(os.Stderr, "GATEWAY_ADMIN_URL is not set; skipping e2e. Run: make test-e2e")
		os.Exit(0)
	}

	gatewaySocketURL = os.Getenv("GATEWAY_SOCKET_URL")
	if gatewaySocketURL == "" {
		gatewaySocketURL = "ws" + gatewayURL[len("http"):] + "/ws/chat"
	}

	if err := waitForStack(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// waitForStack blocks until the gateway answers.
func waitForStack() error {
	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminURL+health.PathLive, nil)
		if err != nil {
			return fmt.Errorf("build probe request: %w", err)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("the gateway never became ready at %s", adminURL)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

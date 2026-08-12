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

	gatewaySocketURL string

	adminURL string
)

const readyTimeout = 30 * time.Second

func TestMain(m *testing.M) {
	gatewayURL = os.Getenv("GATEWAY_URL")
	if gatewayURL == "" {

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

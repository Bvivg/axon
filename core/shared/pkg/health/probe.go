package health

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

// probeTimeout bounds a single self-probe. Generous enough for a loaded machine,
// short enough that a hung probe still returns before the orchestrator's own
// timeout fires.
const probeTimeout = 3 * time.Second

// ProbeFlag is the flag a service exposes so its own binary can probe it.
const ProbeFlag = "healthcheck"

// RunProbeIfRequested probes url and exits when the service was started with
// -healthcheck; otherwise it returns and the service starts normally.
//
// This exists because the runtime image is distroless: there is no shell, no
// wget and no curl for a container healthcheck to call. The alternatives are
// shipping a shell — which is most of the reason distroless is worth using — or
// leaving the images unprobed, so the binary probes itself instead:
//
//	HEALTHCHECK ["/service", "-healthcheck", "http://127.0.0.1:9091/healthz"]
//
// It must be called before any other flag parsing, and before the service
// allocates anything: the probe process shares the image, not the workload.
func RunProbeIfRequested() {
	url := flag.String(ProbeFlag, "", "probe this URL, print the outcome and exit; used by container healthchecks")
	flag.Parse()

	if *url == "" {
		return
	}

	if err := Probe(context.Background(), *url); err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Probe reports whether url answers with a 2xx.
func Probe(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	// The default client, not http.DefaultClient's shared transport: a probe
	// makes one request and exits, so connection reuse buys nothing and a
	// lingering idle connection would outlive the process it belongs to.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}
	return nil
}

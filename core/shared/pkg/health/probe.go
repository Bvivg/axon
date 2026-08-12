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

const probeTimeout = 3 * time.Second

const ProbeFlag = "healthcheck"

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

func Probe(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

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

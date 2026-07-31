//go:build e2e

package e2e

import (
	"net/http"
	"strings"
	"testing"
)

// CORS is enforced below the Connect layer, so these go over raw HTTP. A
// generated client cannot send a preflight — the browser does that on its own —
// which is exactly why this is worth asserting from the outside: nothing else in
// the suite would notice the allow-list breaking.
const (
	allowedOrigin    = "http://localhost:3000"
	disallowedOrigin = "https://evil.example"

	// Any procedure will do; the preflight never reaches the handler.
	procedure = "/axon.auth.v1.AuthService/Login"
)

// preflight returns the status and headers of a preflight from origin. The body
// is closed here rather than by the caller: a preflight has none worth reading,
// and only the headers are the answer.
func preflight(t *testing.T, origin string) (int, http.Header) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodOptions, gatewayURL+procedure, nil)
	if err != nil {
		t.Fatalf("build preflight: %v", err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,connect-protocol-version")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight from %s: %v", origin, err)
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode, resp.Header
}

func TestPreflightFromTheAllowedOrigin(t *testing.T) {
	status, header := preflight(t, allowedOrigin)

	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	if got := header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
		t.Errorf("Allow-Origin = %q, want %q", got, allowedOrigin)
	}
	if got := header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}

	// Without these headers a connect-web client fails in a way that looks like
	// the server is down rather than like a CORS problem.
	allow := header.Get("Access-Control-Allow-Headers")
	for _, required := range []string{"Authorization", "Content-Type", "Connect-Protocol-Version"} {
		if !strings.Contains(allow, required) {
			t.Errorf("Allow-Headers is missing %q: %q", required, allow)
		}
	}

	// Connect carries errors in trailers; a browser cannot read them unless they
	// are exposed.
	expose := header.Get("Access-Control-Expose-Headers")
	for _, required := range []string{"Grpc-Status", "Grpc-Message"} {
		if !strings.Contains(expose, required) {
			t.Errorf("Expose-Headers is missing %q: %q", required, expose)
		}
	}
}

func TestPreflightFromAnyOtherOrigin(t *testing.T) {
	status, header := preflight(t, disallowedOrigin)

	if header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("a disallowed origin was granted CORS headers")
	}
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
}

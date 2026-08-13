package cors_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/services/gateway/internal/cors"
)

const allowed = "http://localhost:3000"

func serve(t *testing.T, req *http.Request, origins ...string) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	if len(origins) == 0 {
		origins = []string{allowed}
	}

	var reached bool
	handler := cors.Middleware(origins)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, reached
}

func preflight(origin string) *http.Request {
	req := httptest.NewRequest(http.MethodOptions, "/axon.auth.v1.AuthService/Login", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,connect-protocol-version")
	return req
}

func TestPreflightFromAnAllowedOrigin(t *testing.T) {
	rec, reached := serve(t, preflight(allowed))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if reached {
		t.Error("the preflight was passed through to the handler")
	}

	h := rec.Header()
	if got := h.Get("Access-Control-Allow-Origin"); got != allowed {
		t.Errorf("Allow-Origin = %q, want %q", got, allowed)
	}
	if got := h.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}

	allowHeaders := h.Get("Access-Control-Allow-Headers")
	for _, required := range []string{
		"Authorization", "Content-Type", "Connect-Protocol-Version", "Connect-Timeout-Ms",
	} {
		if !strings.Contains(allowHeaders, required) {
			t.Errorf("Allow-Headers is missing %q: %q", required, allowHeaders)
		}
	}

	exposed := h.Get("Access-Control-Expose-Headers")
	for _, required := range []string{"Grpc-Status", "Grpc-Message", "X-Correlation-Id"} {
		if !strings.Contains(exposed, required) {
			t.Errorf("Expose-Headers is missing %q: %q", required, exposed)
		}
	}
}

func TestPreflightFromADisallowedOrigin(t *testing.T) {
	rec, reached := serve(t, preflight("https://evil.example"))

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("a disallowed origin was granted CORS headers")
	}
	if reached {
		t.Error("a disallowed preflight reached the handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestActualRequestFromAnAllowedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/axon.auth.v1.AuthService/Login", nil)
	req.Header.Set("Origin", allowed)

	rec, reached := serve(t, req)

	if !reached {
		t.Fatal("the request did not reach the handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != allowed {
		t.Errorf("Allow-Origin = %q, want %q", got, allowed)
	}
}

func TestActualRequestFromADisallowedOriginGetsNoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/axon.auth.v1.AuthService/Login", nil)
	req.Header.Set("Origin", "https://evil.example")

	rec, reached := serve(t, req)

	if !reached {
		t.Fatal("the request did not reach the handler")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("a disallowed origin was granted CORS headers")
	}
}

func TestResponseVariesByOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", allowed)

	rec, _ := serve(t, req)

	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("Vary = %q, want it to include Origin", rec.Header().Get("Vary"))
	}
}

func TestRequestWithoutOriginIsUntouched(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)

	rec, reached := serve(t, req)

	if !reached {
		t.Fatal("a same-origin request was blocked")
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS headers were added to a request with no Origin")
	}
}

func TestMultipleAllowedOrigins(t *testing.T) {
	origins := []string{"http://localhost:3000", "https://axon.example"}

	for _, origin := range origins {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set("Origin", origin)

		rec, _ := serve(t, req, origins...)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Allow-Origin for %q = %q", origin, got)
		}
	}
}

func TestOriginMatchingIsExact(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:3000.evil.example",
		"http://evil.example?http://localhost:3000",
		"http://localhost:30001",
		"https://localhost:3000",
		"http://LOCALHOST:3000",
	} {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set("Origin", origin)

		rec, _ := serve(t, req)

		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q was allowed by a non-exact match", origin)
		}
	}
}

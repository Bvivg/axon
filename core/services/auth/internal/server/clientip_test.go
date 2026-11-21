package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/authn"
)

type fakeResolver struct {
	answers map[string][]string
	calls   int
}

func (f *fakeResolver) lookup(_ context.Context, host string) ([]string, error) {
	f.calls++
	return f.answers[host], nil
}

func seenIP(t *testing.T, trust *ClientIPTrust, r *http.Request) string {
	t.Helper()
	var got string
	trust.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(authn.ClientIPHeader)
	})).ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func forwarded(remote string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/axon.auth.v1.AuthService/Login", nil)
	r.RemoteAddr = remote
	r.Header.Set(authn.ClientIPHeader, "203.0.113.7")
	return r
}

func newTrust(resolver *fakeResolver, now *time.Time) *ClientIPTrust {
	trust := NewClientIPTrust([]string{"gateway"})
	trust.resolve = resolver.lookup
	trust.now = func() time.Time { return *now }
	return trust
}

func TestClientIPIsKeptOnlyFromTheGateway(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	resolver := &fakeResolver{answers: map[string][]string{"gateway": {"172.20.0.5", "172.20.0.6"}}}
	trust := newTrust(resolver, &now)

	cases := map[string]struct {
		remote string
		want   string
	}{
		"gateway replica one": {"172.20.0.5:41000", "203.0.113.7"},
		"gateway replica two": {"172.20.0.6:41000", "203.0.113.7"},
		"another service":     {"172.20.0.9:41000", ""},
		"loopback":            {"127.0.0.1:41000", "203.0.113.7"},
		"garbage address":     {"not-an-address", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := seenIP(t, trust, forwarded(tc.remote)); got != tc.want {
				t.Fatalf("client IP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientIPTrustFollowsTheGatewayAddress(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	resolver := &fakeResolver{answers: map[string][]string{"gateway": {"172.20.0.5"}}}
	trust := newTrust(resolver, &now)
	if got := seenIP(t, trust, forwarded("172.20.0.5:1")); got == "" {
		t.Fatal("gateway refused")
	}

	resolver.answers["gateway"] = []string{"172.20.0.8"}
	now = now.Add(time.Minute)

	if got := seenIP(t, trust, forwarded("172.20.0.5:1")); got != "" {
		t.Fatal("a stale gateway address is still trusted")
	}
	if got := seenIP(t, trust, forwarded("172.20.0.8:1")); got == "" {
		t.Fatal("the new gateway address is not trusted")
	}
}

func TestClientIPTrustDoesNotResolveOnEveryMiss(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	resolver := &fakeResolver{answers: map[string][]string{"gateway": {"172.20.0.5"}}}
	trust := newTrust(resolver, &now)

	for range 50 {
		seenIP(t, trust, forwarded("172.20.0.9:1"))
	}

	if resolver.calls > 1 {
		t.Fatalf("resolved %d times for repeated misses within a second", resolver.calls)
	}
}

func TestClientIPOverMutualTLSNeedsTheGatewayIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	trust := newTrust(&fakeResolver{}, &now)
	for name, want := range map[string]string{"gateway": "203.0.113.7", "chat": ""} {
		r := forwarded("172.20.0.9:1")
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: name}}}}
		if got := seenIP(t, trust, r); got != want {
			t.Fatalf("peer %s: client IP = %q, want %q", name, got, want)
		}
	}
}

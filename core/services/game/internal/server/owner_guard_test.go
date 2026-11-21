package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
)

const testOwnerKey = "0123456789abcdef0123456789abcdef"

func ownerRequest(header string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, gamev1connect.GameOwnerServiceAbortSessionProcedure, strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	if header != "" {
		r.Header.Set(internalrpc.Header, header)
	}
	return r
}

func withPeer(r *http.Request, name string) *http.Request {
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: name}}}}
	return r
}

func guarded(cfg Config) (http.Handler, *bool) {
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	return New(cfg).GuardOwner(next), &reached
}

func TestOwnerServiceRefusesCallersWithoutTheInternalKey(t *testing.T) {
	cases := map[string]struct {
		cfg     Config
		request *http.Request
		allowed bool
	}{
		"no key configured":         {Config{}, ownerRequest(testOwnerKey), false},
		"missing header":            {Config{OwnerKey: testOwnerKey}, ownerRequest(""), false},
		"wrong key":                 {Config{OwnerKey: testOwnerKey}, ownerRequest(strings.Repeat("x", 32)), false},
		"matching key":              {Config{OwnerKey: testOwnerKey}, ownerRequest(testOwnerKey), true},
		"mTLS peer game":            {Config{Secure: true}, withPeer(ownerRequest(""), "game"), true},
		"mTLS peer gateway":         {Config{Secure: true, OwnerKey: testOwnerKey}, withPeer(ownerRequest(testOwnerKey), "gateway"), false},
		"mTLS without a peer":       {Config{Secure: true, OwnerKey: testOwnerKey}, ownerRequest(testOwnerKey), false},
		"key too short to be valid": {Config{OwnerKey: "short"}, ownerRequest("short"), false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			handler, reached := guarded(tc.cfg)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, tc.request)

			if *reached != tc.allowed {
				t.Fatalf("reached = %v, want %v (status %d)", *reached, tc.allowed, rec.Code)
			}
			if !tc.allowed && rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 permission_denied", rec.Code)
			}
			if !tc.allowed && !strings.Contains(rec.Body.String(), connect.CodePermissionDenied.String()) {
				t.Fatalf("body = %s, want a permission_denied Connect error", rec.Body.String())
			}
		})
	}
}

func TestOwnerGuardLeavesThePublicServiceAlone(t *testing.T) {
	handler, reached := guarded(Config{})
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, gamev1connect.GameServiceGetSessionProcedure, strings.NewReader("{}"))

	handler.ServeHTTP(httptest.NewRecorder(), r)

	if !*reached {
		t.Fatal("the guard blocked GameService")
	}
}

func TestPeerOwnerClientPresentsTheKey(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalrpc.Header)
		w.Header().Set("Content-Type", "application/proto")
	}))
	defer srv.Close()
	p := newPeers(srv.Client(), "http", testOwnerKey)

	_, err := p.owner(strings.TrimPrefix(srv.URL, "http://")).AbortSession(t.Context(), connect.NewRequest(&gamev1.AbortSessionRequest{SessionId: "8f0e2b9c-4d7a-4e1b-9c3f-1a2b3c4d5e6f"}))

	if err != nil {
		t.Fatal(err)
	}
	if got != testOwnerKey {
		t.Fatalf("peer sent %q, want the internal key", got)
	}
}

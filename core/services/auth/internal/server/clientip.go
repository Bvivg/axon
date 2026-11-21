package server

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/mtls"
)

const (
	peerAddressTTL     = 30 * time.Second
	peerResolvePause   = time.Second
	peerResolveTimeout = time.Second
)

type ClientIPTrust struct {
	peers   []string
	resolve func(context.Context, string) ([]string, error)
	now     func() time.Time

	mu       sync.Mutex
	known    map[string]struct{}
	resolved time.Time
}

func NewClientIPTrust(peers []string) *ClientIPTrust {
	return &ClientIPTrust{peers: peers, resolve: net.DefaultResolver.LookupHost, now: time.Now}
}

func (t *ClientIPTrust) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(authn.ClientIPHeader) != "" && !t.trusted(r) {
			r.Header.Del(authn.ClientIPHeader)
		}
		next.ServeHTTP(w, r)
	})
}

func (t *ClientIPTrust) trusted(r *http.Request) bool {
	if r.TLS != nil {
		return len(r.TLS.PeerCertificates) > 0 && mtls.Authorize(r.TLS.PeerCertificates[0], t.peers) == nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || t.knownPeer(r.Context(), ip.String())
}

func (t *ClientIPTrust) knownPeer(ctx context.Context, ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	age := t.now().Sub(t.resolved)
	if _, ok := t.known[ip]; ok && age < peerAddressTTL {
		return true
	}
	if t.known != nil && age < peerResolvePause {
		return false
	}
	t.refresh(ctx)
	_, ok := t.known[ip]
	return ok
}

func (t *ClientIPTrust) refresh(ctx context.Context) {
	lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), peerResolveTimeout)
	defer cancel()
	known := map[string]struct{}{}
	for _, peer := range t.peers {
		addresses, err := t.resolve(lookup, peer)
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if ip := net.ParseIP(address); ip != nil {
				known[ip.String()] = struct{}{}
			}
		}
	}
	t.known, t.resolved = known, t.now()
}

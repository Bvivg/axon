package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// ForwardedForHeader carries the chain of proxies a request passed through.
const ForwardedForHeader = "X-Forwarded-For"

// ClientIP returns the address to attribute a request to.
//
// X-Forwarded-For is attacker-controlled. Any client can send one, so trusting
// the leftmost entry — the usual mistake — hands every caller their own rate
// limit bucket for free, and makes the limiter useless against exactly the
// traffic it exists to shape.
//
// The only trustworthy part of the header is what the proxies we actually run
// appended. trustedProxies says how many hops those are, so the caller's address
// is the entry that many positions from the right. With zero, the header is
// ignored entirely and the connection's own address is used — the correct
// default for a service reached directly.
func ClientIP(r *http.Request, trustedProxies int) string {
	if trustedProxies > 0 {
		if ip, ok := forwardedFor(r.Header.Get(ForwardedForHeader), trustedProxies); ok {
			return ip
		}
	}
	return remoteIP(r.RemoteAddr)
}

// forwardedFor picks the entry trustedProxies hops from the right.
func forwardedFor(header string, trustedProxies int) (string, bool) {
	if header == "" {
		return "", false
	}

	parts := strings.Split(header, ",")
	idx := len(parts) - trustedProxies
	if idx < 0 {
		// Fewer entries than proxies we expect: the header is shorter than the
		// path the request took, so nothing in it can be trusted.
		return "", false
	}

	candidate := strings.TrimSpace(parts[idx])
	if candidate == "" || net.ParseIP(candidate) == nil {
		return "", false
	}
	return candidate, true
}

// remoteIP strips the port from a RemoteAddr.
func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Already bare, or something unusual. Either way it is what the
		// connection reported, which is the best available answer.
		return remoteAddr
	}
	return host
}

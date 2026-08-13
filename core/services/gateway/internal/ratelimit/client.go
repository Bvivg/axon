package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

const ForwardedForHeader = "X-Forwarded-For"

func ClientIP(r *http.Request, trustedProxies int) string {
	if trustedProxies > 0 {
		if ip, ok := forwardedFor(r.Header.Get(ForwardedForHeader), trustedProxies); ok {
			return ip
		}
	}
	return remoteIP(r.RemoteAddr)
}

func forwardedFor(header string, trustedProxies int) (string, bool) {
	if header == "" {
		return "", false
	}

	parts := strings.Split(header, ",")
	idx := len(parts) - trustedProxies
	if idx < 0 {

		return "", false
	}

	candidate := strings.TrimSpace(parts[idx])
	if candidate == "" || net.ParseIP(candidate) == nil {
		return "", false
	}
	return candidate, true
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {

		return remoteAddr
	}
	return host
}

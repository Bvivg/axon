package livekitproxy

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1/callsv1connect"
	"github.com/bvivg/axon/core/shared/pkg/internalrpc"
)

type Config struct {
	Upstream    string
	ChatURL     string
	InternalKey string
	Origins     []string
}

func New(cfg Config) (http.Handler, error) {
	target, err := url.Parse(cfg.Upstream)
	if err != nil || target.Scheme != "http" || target.Host == "" || len(cfg.InternalKey) < 32 {
		return nil, errors.New("invalid LiveKit proxy configuration")
	}
	client := callsv1connect.NewMediaAdmissionServiceClient(&http.Client{Timeout: 3 * time.Second}, cfg.ChatURL)
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.SetXForwarded()
		r.Out.URL.Path = strings.TrimPrefix(r.Out.URL.Path, "/livekit")
		r.Out.Header.Del(internalrpc.Header)
		r.Out.Header.Del("Cookie")
	}}
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = func(response *http.Response) error {
		for name := range response.Header {
			if strings.HasPrefix(strings.ToLower(name), "access-control-") {
				response.Header.Del(name)
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "media signaling unavailable", http.StatusBadGateway)
	}
	origins := map[string]bool{}
	for _, origin := range cfg.Origins {
		origins[origin] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/livekit")
		if path != "/rtc" && path != "/rtc/validate" && path != "/rtc/v1" && path != "/rtc/v1/validate" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !origins[origin] {
			http.Error(w, "origin denied", http.StatusForbidden)
			return
		}
		if len(r.URL.RawQuery) > 65536 {
			w.WriteHeader(http.StatusRequestURITooLong)
			return
		}
		raw := r.URL.Query().Get("access_token")
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			bearer := strings.TrimPrefix(auth, "Bearer ")
			if raw != "" && raw != bearer {
				http.Error(w, "ambiguous credentials", http.StatusUnauthorized)
				return
			}
			raw = bearer
		}
		if raw == "" || len(raw) > 16384 {
			http.Error(w, "media authentication required", http.StatusUnauthorized)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		request := connect.NewRequest(&callv1.CheckMediaAdmissionRequest{Token: raw})
		request.Header().Set(internalrpc.Header, cfg.InternalKey)
		_, err := client.CheckMediaAdmission(ctx, request)
		cancel()
		if err != nil {
			http.Error(w, "media admission denied", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

package mediaproxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/bvivg/axon/core/shared/pkg/authn"
)

func New(upstream string, verifier *authn.Verifier) (http.Handler, error) {
	target, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		r.Out.URL.Path = "/internal/media/" + strings.TrimPrefix(r.In.URL.Path, authn.MediaPath+"/")
		r.Out.URL.RawPath = ""
		r.Out.URL.RawQuery = ""
		r.Out.Header.Del("Cookie")
	}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token, err := authn.BearerToken(r.Header)
		if err != nil && r.Header.Get("Authorization") == "" {
			if cookie, cookieErr := r.Cookie(authn.MediaCookieName); cookieErr == nil {
				token, err = cookie.Value, nil
			}
		}
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := verifier.Verify(token); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+token)
		proxy.ServeHTTP(w, r)
	}), nil
}

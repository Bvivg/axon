package cors

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const preflightMaxAge = 12 * time.Hour

var allowedHeaders = []string{
	"Content-Type",
	"Authorization",
	"X-Correlation-Id",
	"Connect-Protocol-Version",
	"Connect-Timeout-Ms",
	"Grpc-Timeout",
	"X-Grpc-Web",
	"X-User-Agent",
}

var exposedHeaders = []string{
	"Content-Type",
	"X-Correlation-Id",
	"Connect-Protocol-Version",
	"Grpc-Status",
	"Grpc-Message",
	"Grpc-Status-Details-Bin",
}

func Middleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowHeaders := strings.Join(allowedHeaders, ", ")
	exposeHeaders := strings.Join(exposedHeaders, ", ")
	maxAge := strconv.Itoa(int(preflightMaxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin == "" || !slices.Contains(allowedOrigins, origin) {

				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)

			header.Add("Vary", "Origin")

			header.Set("Access-Control-Allow-Credentials", "true")
			header.Set("Access-Control-Expose-Headers", exposeHeaders)

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				header.Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
				header.Set("Access-Control-Allow-Headers", allowHeaders)
				header.Set("Access-Control-Max-Age", maxAge)
				header.Add("Vary", "Access-Control-Request-Method")
				header.Add("Vary", "Access-Control-Request-Headers")

				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

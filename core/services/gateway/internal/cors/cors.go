// Package cors implements the gateway's cross-origin policy.
//
// The allow-list is explicit and a wildcard is impossible to configure. The
// gateway is the trust boundary and it serves credentialed requests; `*` there
// would let any page on the internet call the API as whoever is signed in.
package cors

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// preflightMaxAge is how long a browser may cache a preflight result. Long
// enough that an OPTIONS is not paid per request, short enough that a policy
// change takes effect the same day.
const preflightMaxAge = 12 * time.Hour

// allowedHeaders is what a browser may send.
//
// The Connect ones are not optional: connect-web puts the protocol version and
// timeout in headers, and gRPC-Web adds its own. A preflight that omits them
// fails in a way that looks like the server is down.
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

// exposedHeaders is what browser JavaScript may read off a response. Without
// these, a Connect client cannot see the trailers that carry an error.
var exposedHeaders = []string{
	"Content-Type",
	"X-Correlation-Id",
	"Connect-Protocol-Version",
	"Grpc-Status",
	"Grpc-Message",
	"Grpc-Status-Details-Bin",
}

// Middleware answers preflights and adds the response headers.
//
// An origin that is not on the list gets no CORS headers at all rather than an
// error: that is what the specification calls for, and the browser produces a
// clearer message than any status this could invent.
func Middleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowHeaders := strings.Join(allowedHeaders, ", ")
	exposeHeaders := strings.Join(exposedHeaders, ", ")
	maxAge := strconv.Itoa(int(preflightMaxAge.Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin == "" || !slices.Contains(allowedOrigins, origin) {
				// Not a cross-origin request, or not one we serve. Preflights
				// still have to be answered, or the browser reports a network
				// error instead of a policy failure.
				if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)
			// The response varies by origin, so a cache must not serve one
			// origin's response to another.
			header.Add("Vary", "Origin")
			// Credentials are on because the refresh token travels in a cookie.
			// This is exactly why the origin is echoed from an allow-list and
			// never wildcarded: the two are incompatible by specification, and
			// browsers refuse the combination outright.
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

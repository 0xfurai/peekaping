package httpx

import (
	"net/http"
	"runtime/debug"
	"slices"

	"peekaping/internal/reporting"

	"github.com/felixge/httpsnoop"
	"go.uber.org/zap"
)

// Recovery answers a handler's panic with 500, logs it with its stack, and reports it unless an
// admin opted out of error reports. http.ErrAbortHandler is net/http's way for a handler to abort its
// response, so it goes on to net/http, which closes the connection without a log.
func Recovery(logger *zap.SugaredLogger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					if err == http.ErrAbortHandler {
						panic(err)
					}
					stack := debug.Stack()
					Log(r.Context(), logger).Errorw("HTTP handler panic", "error", err, "stack", string(stack))
					reporting.CapturePanic("http", err, stack)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// quietRoutes are the endpoints that monitoring and probes poll, which Logger logs at debug level:
// health and metrics, and the long poll a probe makes for its assignments, which comes about
// every 25 seconds from each probe instance.
var quietRoutes = []string{"/health", "/api/v1/health", "/metrics", "/probe/v1/assignments"}

// Logger logs each request once it is done, after RequestID. It logs the pattern of the route
// that served the request, empty when none did, and never the path or the query, which can hold
// a secret such as a push token. The requests to quietRoutes go to debug level, and the others
// to info level; a request that no route served is matched by its path. The client address it
// logs is the one proxies resolves. A request that an outer Logger already handles is passed
// through, so that it is logged once.
func Logger(logger *zap.SugaredLogger, proxies *TrustedProxies) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := requestInfoFrom(r.Context())
			if info != nil {
				if info.logged {
					next.ServeHTTP(w, r)
					return
				}
				info.logged = true
			}
			// Preserve optional ResponseWriter interfaces for streaming and upgrades.
			metrics := httpsnoop.CaptureMetrics(next, w, r)
			route := ""
			if info != nil {
				route = info.route
			}
			// With no route, as when the web gateway serves the request itself, the path decides.
			// A client can make its path quiet, which only lowers the level of its own request.
			quiet := route
			if quiet == "" {
				quiet = r.URL.Path
			}
			level := zap.InfoLevel
			if slices.Contains(quietRoutes, quiet) {
				level = zap.DebugLevel
			}
			Log(r.Context(), logger).Logw(level, "HTTP request", "method", r.Method, "route", route,
				"status", metrics.Code, "client_ip", proxies.ClientIP(r), "size", metrics.Written, "duration", metrics.Duration)
		})
	}
}

// LimitBody answers 413 to a request that declares a Content-Length over max bytes, without
// running the handler. It makes reading any other body fail with *http.MaxBytesError once it
// passes max bytes, so that a handler (or a guard that reads the body before it) never holds
// more than that.
func LimitBody(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > max {
				JSON(w, http.StatusRequestEntityTooLarge, ErrorBody{Message: "request body too large"})
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS lets a page on one of origins read the API's responses. A request from any other origin
// is served as it is, without CORS headers, so its browser won't hand the response to the page.
// A request with no Origin, or from the host it is sent to, isn't cross-origin and is left
// alone. Responses never carry Access-Control-Allow-Credentials: the API's auth is a bearer
// header, not a cookie. With a list of origins every response gets Vary: Origin, since what it
// carries depends on the request's Origin and a cache must not serve one origin's copy to another.
func CORS(origins *CORSOrigins) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origins.listed() {
				w.Header().Add("Vary", "Origin")
			}
			origin := r.Header.Get("Origin")
			sameOrigin := origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
			allowOrigin := origins.AllowOrigin(origin)
			if sameOrigin || allowOrigin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS,PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Origin,X-Requested-With,Content-Type,Accept,Authorization,X-Request-ID")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Access-Control-Expose-Headers", "Authorization, X-Request-ID")
			next.ServeHTTP(w, r)
		})
	}
}

package httpx

import (
	"net/http"
	"slices"

	"github.com/felixge/httpsnoop"
	"go.uber.org/zap"
)

func Recovery(logger *zap.SugaredLogger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					Log(r.Context(), logger).Errorw("HTTP handler panic", "error", err)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// quietRoutes are the routes that monitoring polls, which Logger logs at debug level.
var quietRoutes = []string{"/health", "/api/v1/health", "/metrics"}

// Logger logs each request once it is done, after RequestID. It logs the pattern of the route
// that served the request, empty when none did, and never the path or the query, which can hold
// a secret such as a push token. The requests to quietRoutes go to debug level, and the others
// to info level. The client address it logs is the one proxies resolves. A request that an outer
// Logger already handles is passed through, so that it is logged once.
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
			level := zap.InfoLevel
			if slices.Contains(quietRoutes, route) {
				level = zap.DebugLevel
			}
			Log(r.Context(), logger).Logw(level, "HTTP request", "method", r.Method, "route", route,
				"status", metrics.Code, "client_ip", proxies.ClientIP(r), "size", metrics.Written, "duration", metrics.Duration)
		})
	}
}

// LimitBody makes reading a request body fail with *http.MaxBytesError once it passes max bytes,
// so that a handler (or a guard that reads the body before it) never holds more than that.
func LimitBody(max int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS retains the existing wildcard-origin policy; PP-55 owns policy changes.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS,PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Origin,X-Requested-With,Content-Type,Accept,Authorization")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Access-Control-Expose-Headers", "Authorization, X-Request-ID")
		next.ServeHTTP(w, r)
	})
}

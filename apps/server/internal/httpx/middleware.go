package httpx

import (
	"net/http"

	"github.com/felixge/httpsnoop"
	"go.uber.org/zap"
)

func Recovery(logger *zap.SugaredLogger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Errorw("HTTP handler panic", "error", err)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logger logs each request. The client address it logs is the one proxies resolves.
func Logger(logger *zap.SugaredLogger, proxies *TrustedProxies) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Preserve optional ResponseWriter interfaces for streaming and upgrades.
			metrics := httpsnoop.CaptureMetrics(next, w, r)
			path := r.URL.Path
			if r.URL.RawQuery != "" {
				path += "?" + r.URL.RawQuery
			}
			logger.Infow("HTTP request", "method", r.Method, "path", path,
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

// CORS lets a page on one of origins read the API's responses. A request from any other origin
// is served as it is, without CORS headers, so its browser won't hand the response to the page.
// A request with no Origin, or from the host it is sent to, isn't cross-origin and is left
// alone. Responses never carry Access-Control-Allow-Credentials: the API's auth is a bearer
// header, not a cookie.
func CORS(origins *CORSOrigins) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			sameOrigin := origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
			allowOrigin := origins.AllowOrigin(origin)
			if sameOrigin || allowOrigin == "" {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS,PATCH")
				w.Header().Set("Access-Control-Allow-Headers", "Origin,X-Requested-With,Content-Type,Accept,Authorization")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Access-Control-Expose-Headers", "Authorization")
			next.ServeHTTP(w, r)
		})
	}
}

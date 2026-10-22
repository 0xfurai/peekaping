package httpx

import (
	"net/http"
	"runtime/debug"

	"peekaping/internal/reporting"

	"github.com/felixge/httpsnoop"
	"go.uber.org/zap"
)

// Recovery answers a handler's panic with 500, logs it with its stack, and reports it when an admin
// opted in to error reports. http.ErrAbortHandler is net/http's way for a handler to abort its
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
					logger.Errorw("HTTP handler panic", "error", err, "stack", string(stack))
					reporting.CapturePanic("http", err, stack)
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
		w.Header().Set("Access-Control-Expose-Headers", "Authorization")
		next.ServeHTTP(w, r)
	})
}

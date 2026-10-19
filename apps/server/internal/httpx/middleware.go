package httpx

import (
	"net/http"
	"time"

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
func Logger(logger *zap.SugaredLogger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			next.ServeHTTP(w, r)
			logger.Infow("HTTP request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(started))
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

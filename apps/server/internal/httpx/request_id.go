package httpx

import (
	"net/http"

	"github.com/google/uuid"
)

const (
	requestIDHeader = "X-Request-ID"
	maxRequestIDLen = 64
)

// RequestID gives each request an ID and sets it as the X-Request-ID header of the response. It
// keeps the request's own X-Request-ID when that is up to 64 characters of A-Z, a-z, 0-9, '.',
// '_' and '-', and generates one otherwise.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range []byte(id) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

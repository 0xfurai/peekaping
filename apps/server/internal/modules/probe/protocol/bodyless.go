package protocol

import "net/http"

// Bodyless takes the Content-Type off a reply that can have no body, 204 and 304. The framework
// sets a Content-Type on every reply it writes, but a reply with nothing in it has no type, and a
// probe that polls every few seconds gains from one header fewer.
func Bodyless(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&bodylessWriter{ResponseWriter: w}, r)
	})
}

type bodylessWriter struct {
	http.ResponseWriter
}

func (b *bodylessWriter) WriteHeader(status int) {
	if status == http.StatusNoContent || status == http.StatusNotModified {
		b.Header().Del("Content-Type")
	}
	b.ResponseWriter.WriteHeader(status)
}

// Flush lets a handler that streams do so through the wrapper
func (b *bodylessWriter) Flush() {
	if flusher, ok := b.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer
func (b *bodylessWriter) Unwrap() http.ResponseWriter { return b.ResponseWriter }

package httpx

import (
	"net/http"
	"time"
)

// Timeouts bound how long a client may take over a request.
type Timeouts struct {
	// ReadHeader is how long a client has to send a request's headers
	ReadHeader time.Duration
}

// DefaultTimeouts are what both binaries serve with.
func DefaultTimeouts() Timeouts {
	return Timeouts{ReadHeader: 10 * time.Second}
}

// maxHeaderBytes is the size of a request's header block the server reads before it answers 431
const maxHeaderBytes = 64 << 10

// NewServer is the HTTP server both binaries serve the API with, so that they hold clients to
// the same limits.
func NewServer(addr string, handler http.Handler, t Timeouts) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: t.ReadHeader,
		MaxHeaderBytes:    maxHeaderBytes,
	}
}

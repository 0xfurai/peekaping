package shutdown

import (
	"net/http"
	"sync"
)

// RunningHandlers serves its handler and counts the requests it is serving. A handler that
// hijacks its connection, as the websocket's does, counts until it returns, not until the
// connection closes. The HTTP server doesn't wait for the handlers of the connections it drops
// or that were hijacked, so Done is how a shutdown knows every handler has returned and is done
// with what it is about to close.
type RunningHandlers struct {
	handler http.Handler

	mu      sync.Mutex
	running int
	idle    []chan struct{} // closed once running drops to zero
}

// NewRunningHandlers returns a RunningHandlers that serves h
func NewRunningHandlers(h http.Handler) *RunningHandlers {
	return &RunningHandlers{handler: h}
}

// ServeHTTP serves the request with the handler, counted as running until the handler returns
func (h *RunningHandlers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.running++
	h.mu.Unlock()
	defer h.returned()
	h.handler.ServeHTTP(w, r)
}

func (h *RunningHandlers) returned() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running--
	if h.running == 0 {
		for _, idle := range h.idle {
			close(idle)
		}
		h.idle = nil
	}
}

// Done returns a channel that closes once no handler is running
func (h *RunningHandlers) Done() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	idle := make(chan struct{})
	if h.running == 0 {
		close(idle)
	} else {
		h.idle = append(h.idle, idle)
	}
	return idle
}

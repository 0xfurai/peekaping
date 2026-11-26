package shutdown

import (
	"net/http"
	"sync"
)

// inFlight counts the work in flight, and tells when none is left
type inFlight struct {
	mu      sync.Mutex
	running int
	idle    []chan struct{} // closed once running drops to zero
}

func (f *inFlight) started() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running++
}

func (f *inFlight) returned() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running--
	if f.running == 0 {
		for _, idle := range f.idle {
			close(idle)
		}
		f.idle = nil
	}
}

// Done returns a channel that closes once nothing is running
func (f *inFlight) Done() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	idle := make(chan struct{})
	if f.running == 0 {
		close(idle)
	} else {
		f.idle = append(f.idle, idle)
	}
	return idle
}

// RunningHandlers serves its handler and counts the requests it is serving. A handler that
// hijacks its connection, as the websocket's does, counts until it returns, not until the
// connection closes. The HTTP server doesn't wait for the handlers of the connections it drops
// or that were hijacked, so Done is how a shutdown knows every handler has returned and is done
// with what it is about to close.
type RunningHandlers struct {
	inFlight
	handler http.Handler
}

// NewRunningHandlers returns a RunningHandlers that serves h
func NewRunningHandlers(h http.Handler) *RunningHandlers {
	return &RunningHandlers{handler: h}
}

// ServeHTTP serves the request with the handler, counted as running until the handler returns
func (h *RunningHandlers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.started()
	defer h.returned()
	h.handler.ServeHTTP(w, r)
}

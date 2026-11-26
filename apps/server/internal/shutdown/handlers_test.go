package shutdown_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingHandler answers 204 once released. entered gets a value each time a request arrives.
type blockingHandler struct {
	entered chan struct{}
	release chan struct{}
}

func newBlockingHandler() *blockingHandler {
	return &blockingHandler{entered: make(chan struct{}, 2), release: make(chan struct{}, 2)}
}

func (h *blockingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.entered <- struct{}{}
	<-h.release
	w.WriteHeader(http.StatusNoContent)
}

// serve sends a request through handler in the background and returns a channel that closes once
// handler has returned
func serve(handler http.Handler) <-chan struct{} {
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	return returned
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func receive(t *testing.T, c <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(wait):
		require.FailNow(t, what)
	}
}

func TestRunningHandlers_ServesTheRequestsWithItsHandler(t *testing.T) {
	handlers := shutdown.NewRunningHandlers(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	recorder := httptest.NewRecorder()

	handlers.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/brew", nil))

	assert.Equal(t, http.StatusTeapot, recorder.Code)
	assert.Equal(t, "/brew", recorder.Body.String())
}

func TestRunningHandlers_DoneIsClosedWhileNoHandlerIsRunning(t *testing.T) {
	handlers := shutdown.NewRunningHandlers(http.NotFoundHandler())

	assert.True(t, isClosed(handlers.Done()), "no handler is running yet")

	handlers.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	assert.True(t, isClosed(handlers.Done()), "the handler has returned")
}

func TestRunningHandlers_DoneClosesOnceTheLastRunningHandlerReturns(t *testing.T) {
	blocking := newBlockingHandler()
	handlers := shutdown.NewRunningHandlers(blocking)
	first := serve(handlers)
	second := serve(handlers)
	receive(t, blocking.entered, "the first request didn't reach the handler")
	receive(t, blocking.entered, "the second request didn't reach the handler")
	// Asked for while both run: every one of them closes
	done, alsoDone := handlers.Done(), handlers.Done()

	assert.False(t, isClosed(done), "both handlers are running")

	blocking.release <- struct{}{}
	// One of the two returned: they are the same handler, so either may be the one released
	select {
	case <-first:
	case <-second:
	case <-time.After(wait):
		require.FailNow(t, "the released handler didn't return")
	}
	assert.False(t, isClosed(done), "one handler is still running")
	assert.False(t, isClosed(alsoDone), "one handler is still running")

	blocking.release <- struct{}{}
	receive(t, done, "Done didn't close once the last handler returned")
	receive(t, alsoDone, "the second Done didn't close once the last handler returned")
	assert.True(t, isClosed(handlers.Done()), "no handler is running any more")
}

func TestRunningHandlers_DoneStaysOpenForAHandlerThatStartsAfterItWasAskedFor(t *testing.T) {
	blocking := newBlockingHandler()
	handlers := shutdown.NewRunningHandlers(blocking)

	done := handlers.Done()
	assert.True(t, isClosed(done), "idle when asked")

	returned := serve(handlers)
	receive(t, blocking.entered, "the request didn't reach the handler")
	later := handlers.Done()
	assert.False(t, isClosed(later), "a handler is running")

	blocking.release <- struct{}{}
	receive(t, returned, "the handler didn't return")
	receive(t, later, "Done didn't close once the handler returned")
}

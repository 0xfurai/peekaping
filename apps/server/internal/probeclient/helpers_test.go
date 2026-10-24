package probeclient_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const probeToken = "pkp_test-token"

// newServer starts a stand-in for the probe server that serves each request with handler
func newServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// readJSON decodes a request body the client gzipped
func readJSON(t *testing.T, r *http.Request, into any) {
	t.Helper()
	require.Equal(t, "gzip", r.Header.Get("Content-Encoding"), "the client gzips its request bodies")
	zr, err := gzip.NewReader(r.Body)
	require.NoError(t, err)
	require.NoError(t, json.NewDecoder(zr).Decode(into))
}

// reply writes body as JSON
func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// fail writes an error reply as the API does. retryAfter is the Retry-After header, if any.
func fail(w http.ResponseWriter, status int, message string, retryAfter time.Duration) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter/time.Second)))
	}
	reply(w, status, probeproto.ErrorResponse{Message: message})
}

// welcomeFrom is a hello reply from a server whose clock reads serverTime
func welcomeFrom(serverTime time.Time) probeproto.HelloResponse {
	return probeproto.HelloResponse{
		ProtocolVersion: 1, ServerVersion: "0.0.48", ServerTime: serverTime,
		Probe:         probeproto.ProbeInfo{ID: "p1", Name: "Frankfurt", Location: "eu-central"},
		PollTimeoutMs: 25000, FlushIntervalMs: 2000, BatchMax: 500, BufferMax: 10000,
	}
}

// sleeper stands in for the session's waits: it returns at once and remembers how long it was
// asked to wait. after, if set, runs on each wait with the count of waits so far.
type sleeper struct {
	mu    sync.Mutex
	slept []time.Duration
	after func(n int, d time.Duration)
}

func (s *sleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.slept = append(s.slept, d)
	n := len(s.slept)
	s.mu.Unlock()
	if s.after != nil {
		s.after(n, d)
	}
	return ctx.Err()
}

func (s *sleeper) waits() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.slept...)
}

func nopLogger() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// observedLogger is a logger whose entries a test can read
func observedLogger() (*zap.SugaredLogger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core).Sugar(), logs
}

// logged are the entries a logger saw at a level, as their message and fields written out
func logged(logs *observer.ObservedLogs, level string) []string {
	var out []string
	for _, entry := range logs.All() {
		if entry.Level.String() == level {
			out = append(out, fmt.Sprintf("%s %v", entry.Message, entry.ContextMap()))
		}
	}
	return out
}

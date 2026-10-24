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

	"github.com/stretchr/testify/assert"
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
	// block makes a wait last until its context ends
	block bool
	// gate, if set, makes a wait last until it receives from it or its context ends
	gate chan struct{}
}

func (s *sleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.slept = append(s.slept, d)
	n := len(s.slept)
	s.mu.Unlock()
	if s.after != nil {
		s.after(n, d)
	}
	if s.block {
		<-ctx.Done()
	}
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
		}
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

// holdingSleeper is a sleeper that, after it notes a wait, does not return until its context ends,
// for a test that wants the probe parked in its backoff
func holdingSleeper() *sleeper {
	return &sleeper{block: true}
}

// pollStep is how the stand-in server answers one long poll
type pollStep func(w http.ResponseWriter)

// assignmentServer is a stand-in for the probe server that welcomes every hello and answers the
// n-th poll with the n-th step. Once the steps are used up a poll is held until the probe hangs up,
// as a long poll with nothing to say is. It notes the hellos and polls in the order they came.
type assignmentServer struct {
	*httptest.Server

	mu       sync.Mutex
	events   []string
	versions []string
	polls    int
	stored   []probeproto.Result
}

func newAssignmentServer(t *testing.T, steps ...pollStep) *assignmentServer {
	t.Helper()
	return newAssignmentServerWith(t, nil, steps...)
}

// newAssignmentServerWith is newAssignmentServer for a server whose hello states other limits
func newAssignmentServerWith(t *testing.T, limits func(*probeproto.HelloResponse), steps ...pollStep) *assignmentServer {
	t.Helper()
	s := &assignmentServer{}
	s.Server = newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/probe/v1/hello":
			s.note("hello")
			welcome := welcomeFrom(time.Now())
			if limits != nil {
				limits(&welcome)
			}
			reply(w, http.StatusOK, welcome)
		case "/probe/v1/assignments":
			s.mu.Lock()
			n := s.polls
			s.polls++
			s.events = append(s.events, "poll")
			s.versions = append(s.versions, r.URL.Query().Get("version"))
			s.mu.Unlock()
			if n < len(steps) {
				steps[n](w)
				return
			}
			<-r.Context().Done()
		case "/probe/v1/results":
			var req probeproto.ResultsRequest
			readJSON(t, r, &req)
			outcomes := make([]probeproto.ResultOutcome, len(req.Results))
			for i, result := range req.Results {
				outcomes[i] = probeproto.ResultOutcome{ID: result.ID, Accepted: true}
			}
			s.mu.Lock()
			s.events = append(s.events, "results")
			s.stored = append(s.stored, req.Results...)
			s.mu.Unlock()
			reply(w, http.StatusOK, probeproto.ResultsResponse{Results: outcomes})
		case "/probe/v1/bye":
			s.note("bye")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	return s
}

// storedResults are the results the server has accepted, in order
func (s *assignmentServer) storedResults() []probeproto.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]probeproto.Result(nil), s.stored...)
}

func (s *assignmentServer) note(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

// sequence is the hellos and polls the server has seen, in order
func (s *assignmentServer) sequence() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

// polledVersions are the versions the polls named, in order
func (s *assignmentServer) polledVersions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.versions...)
}

// snapshot answers a poll with the probe's monitors at a version
func snapshot(version string, monitors ...probeproto.Monitor) pollStep {
	return func(w http.ResponseWriter) {
		reply(w, http.StatusOK, probeproto.AssignmentsResponse{Version: version, Monitors: monitors})
	}
}

// unchanged answers a poll with 304
func unchanged() pollStep {
	return func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotModified) }
}

// failing answers a poll with an error
func failing(status int, message string, retryAfter time.Duration) pollStep {
	return func(w http.ResponseWriter) { fail(w, status, message, retryAfter) }
}

// dropped cuts a poll's answer short and closes the connection, which the probe sees as a network
// error. (A connection closed before any answer is tried again by net/http, behind the probe's back.)
func dropped() pollStep {
	return func(w http.ResponseWriter) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"version\":"))
		_ = conn.Close()
	}
}

var monitorTime = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// httpMonitor is a monitor of the probe's assignment
func httpMonitor(id string, mods ...func(*probeproto.Monitor)) probeproto.Monitor {
	m := probeproto.Monitor{
		ID: id, Name: "Monitor " + id, Type: "http", Interval: 60, Timeout: 16, MaxRetries: 3, RetryInterval: 60, ResendInterval: 10,
		Config: `{"url":"https://example.com"}`, UpdatedAt: monitorTime,
	}
	for _, mod := range mods {
		mod(&m)
	}
	return m
}

// behindProxy gives the monitor a proxy
func behindProxy(host string) func(*probeproto.Monitor) {
	return func(m *probeproto.Monitor) {
		m.Proxy = &probeproto.Proxy{ID: "px1", Protocol: "socks5", Host: host, Port: 1080, Auth: true, Username: "scout", Password: "s3cret", UpdatedAt: monitorTime}
	}
}

// expectSequence waits for the server to have seen the hellos and polls in this order
func (s *assignmentServer) expectSequence(t *testing.T, want []string, msgAndArgs ...any) {
	t.Helper()
	require.Eventually(t, func() bool { return len(s.sequence()) >= len(want) }, 5*time.Second, time.Millisecond, msgAndArgs...)
	assert.Equal(t, want, s.sequence(), msgAndArgs...)
}

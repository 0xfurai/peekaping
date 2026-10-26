package probeclient_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/modules/certificate"
	"peekaping/internal/modules/ingester"
	"peekaping/internal/modules/shared"
	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sink is where the probe's scheduler puts its results
var _ ingester.Service = (*probeclient.Sink)(nil)

// resultsServer is a stand-in for the probe server. It welcomes every hello with the tunables
// given, and stores the results it accepts, in the order it got them. While it is cut it answers
// every call 503. answer, if set, replaces the way it answers a batch.
type resultsServer struct {
	*httptest.Server
	tunables probeproto.HelloResponse
	cut      atomic.Bool

	mu        sync.Mutex
	answer    func(attempt int, batch []probeproto.Result, w http.ResponseWriter)
	attempts  [][]probeproto.Result
	stored    []probeproto.Result
	bodySizes []int
	events    []string
}

func newResultsServer(t *testing.T, mod func(*probeproto.HelloResponse)) *resultsServer {
	t.Helper()
	s := &resultsServer{tunables: welcomeFrom(time.Now())}
	if mod != nil {
		mod(&s.tunables)
	}
	s.Server = newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if s.cut.Load() {
			fail(w, http.StatusServiceUnavailable, "down", 0)
			return
		}
		switch r.URL.Path {
		case "/probe/v1/hello":
			s.note("hello")
			reply(w, http.StatusOK, s.tunables)
		case "/probe/v1/results":
			s.serveResults(t, w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return s
}

func (s *resultsServer) note(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *resultsServer) serveResults(t *testing.T, w http.ResponseWriter, r *http.Request) {
	assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"), "the client gzips the batch")
	zr, err := gzip.NewReader(r.Body)
	require.NoError(t, err)
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	var req probeproto.ResultsRequest
	require.NoError(t, json.Unmarshal(raw, &req))

	s.mu.Lock()
	s.events = append(s.events, "results")
	s.attempts = append(s.attempts, req.Results)
	s.bodySizes = append(s.bodySizes, len(raw))
	attempt, answer := len(s.attempts), s.answer
	s.mu.Unlock()

	if answer != nil {
		answer(attempt, req.Results, w)
		return
	}
	s.accept(w, req.Results)
}

// accept stores the batch and answers every result accepted
func (s *resultsServer) accept(w http.ResponseWriter, batch []probeproto.Result) {
	outcomes := make([]probeproto.ResultOutcome, len(batch))
	for i, result := range batch {
		outcomes[i] = probeproto.ResultOutcome{ID: result.ID, Accepted: true}
	}
	s.mu.Lock()
	s.stored = append(s.stored, batch...)
	s.mu.Unlock()
	reply(w, http.StatusOK, probeproto.ResultsResponse{Results: outcomes})
}

func (s *resultsServer) storedResults() []probeproto.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]probeproto.Result(nil), s.stored...)
}

func (s *resultsServer) attemptCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attempts)
}

func (s *resultsServer) batchSizes() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	sizes := make([]int, len(s.attempts))
	for i, batch := range s.attempts {
		sizes[i] = len(batch)
	}
	return sizes
}

func (s *resultsServer) sequence() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

// newSink creates a sink that sends to the server through a session whose waits are the sleeper's
func newSink(server *resultsServer, pause *sleeper, mod func(*probeclient.SessionConfig)) (*probeclient.Sink, *probeclient.Session) {
	session := newSession(server.URL, func(c *probeclient.SessionConfig) {
		c.Sleep = pause.Sleep
		if mod != nil {
			mod(c)
		}
	})
	return probeclient.NewSink(session), session
}

// runSink runs the sink's flusher until the test ends
func runSink(t *testing.T, sink *probeclient.Sink) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sink.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

var checkStart = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

// checked is the result of a check of a monitor that started n × 20 s after checkStart and took 120 ms
func checked(monitorID string, n int, status shared.MonitorStatus, message string) *ingester.Result {
	start := checkStart.Add(time.Duration(n) * 20 * time.Second)
	return &ingester.Result{
		MonitorID: monitorID, MonitorName: "ignored", MonitorType: "http", Status: status, Message: message, PingMs: 120,
		StartTime: start, EndTime: start.Add(120 * time.Millisecond),
	}
}

func ingest(t *testing.T, sink *probeclient.Sink, results ...*ingester.Result) {
	t.Helper()
	for _, r := range results {
		require.NoError(t, sink.Ingest(context.Background(), r))
	}
}

// withoutIDs separates the ids of results from the rest
func withoutIDs(results []probeproto.Result) (rest []probeproto.Result, ids []string) {
	for _, r := range results {
		ids = append(ids, r.ID)
		r.ID = ""
		rest = append(rest, r)
	}
	return rest, ids
}

func TestSink_ServerCutThenResultsArriveInOrderWithTheirTimes(t *testing.T) {
	server := newResultsServer(t, nil)
	pause := &sleeper{after: func(int, time.Duration) { time.Sleep(time.Millisecond) }}
	sink, _ := newSink(server, pause, nil)
	runSink(t, sink)
	server.cut.Store(true)

	ingest(t, sink,
		checked("m1", 0, shared.MonitorStatusUp, "200 OK"),
		checked("m2", 0, shared.MonitorStatusDown, "connection refused"),
		checked("m1", 1, shared.MonitorStatusUp, "200 OK"),
	)
	require.Eventually(t, func() bool { return len(pause.waits()) >= 3 }, 5*time.Second, time.Millisecond, "the probe tries again and again")
	ingest(t, sink,
		checked("m2", 1, shared.MonitorStatusDown, "connection refused"),
		checked("m1", 2, shared.MonitorStatusUp, "200 OK"),
		checked("m2", 2, shared.MonitorStatusUp, "200 OK"),
	)
	assert.Empty(t, server.storedResults(), "the server has seen nothing while it was cut")

	server.cut.Store(false)

	require.Eventually(t, func() bool { return len(server.storedResults()) == 6 }, 5*time.Second, time.Millisecond)
	stored, ids := withoutIDs(server.storedResults())
	at := func(n int) (time.Time, time.Time) {
		start := checkStart.Add(time.Duration(n) * 20 * time.Second)
		return start, start.Add(120 * time.Millisecond)
	}
	result := func(monitorID, status, message string, n int) probeproto.Result {
		start, end := at(n)
		return probeproto.Result{MonitorID: monitorID, Status: status, Message: message, PingMs: 120, StartTime: start, EndTime: end}
	}
	assert.Equal(t, []probeproto.Result{
		result("m1", "up", "200 OK", 0),
		result("m2", "down", "connection refused", 0),
		result("m1", "up", "200 OK", 1),
		result("m2", "down", "connection refused", 1),
		result("m1", "up", "200 OK", 2),
		result("m2", "up", "200 OK", 2),
	}, stored, "the results arrive oldest first, with the times of their checks")
	assert.IsIncreasing(t, ids, "and their ids say the same")
	require.Eventually(t, func() bool { return sink.Buffered() == 0 }, 5*time.Second, time.Millisecond, "the server's answer empties the buffer")
	assert.Zero(t, sink.Dropped())
}

func TestSink_RejectedAreDropped(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(_ int, batch []probeproto.Result, w http.ResponseWriter) {
		reply(w, http.StatusOK, probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{
			{ID: batch[0].ID, Accepted: true},
			{ID: batch[1].ID, Reason: probeproto.ReasonUnknownMonitor},
			{ID: batch[2].ID, Reason: probeproto.ReasonTooOld},
			{ID: batch[3].ID, Reason: probeproto.ReasonDuplicate},
		}})
	}
	sink, _ := newSink(server, &sleeper{}, nil)
	ingest(t, sink,
		checked("m1", 0, shared.MonitorStatusUp, "ok"),
		checked("gone", 0, shared.MonitorStatusUp, "ok"),
		checked("m1", 1, shared.MonitorStatusUp, "ok"),
		checked("m1", 2, shared.MonitorStatusUp, "ok"),
	)

	require.NoError(t, sink.Flush(context.Background()))
	require.NoError(t, sink.Flush(context.Background()))

	assert.Equal(t, []int{4}, server.batchSizes(), "the batch is sent once, and nothing is left to send again")
	assert.Zero(t, sink.Buffered())
}

func TestSink_UnansweredResultsAreKeptAndSentAgain(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(attempt int, batch []probeproto.Result, w http.ResponseWriter) {
		if attempt == 1 {
			// the server took the first result and failed on the rest
			server.mu.Lock()
			server.stored = append(server.stored, batch[0])
			server.mu.Unlock()
			reply(w, http.StatusOK, probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{{ID: batch[0].ID, Accepted: true}}})
			return
		}
		server.accept(w, batch)
	}
	sink, _ := newSink(server, &sleeper{}, nil)
	ingest(t, sink,
		checked("m1", 0, shared.MonitorStatusUp, "first"),
		checked("m1", 1, shared.MonitorStatusUp, "second"),
		checked("m1", 2, shared.MonitorStatusUp, "third"),
	)

	require.NoError(t, sink.Flush(context.Background()))

	assert.Equal(t, []int{3, 2}, server.batchSizes())
	stored := server.storedResults()
	require.Len(t, stored, 3)
	assert.Equal(t, []string{"first", "second", "third"}, []string{stored[0].Message, stored[1].Message, stored[2].Message})
}

// The server answers a batch 503 with a Retry-After when another request holds the id of one of its
// results (it may be storing it, or have died storing it). That is no answer about the results: the
// probe keeps the whole batch, waits as asked, says hello again and sends the same batch. It is the
// server's way of not answering duplicate for a result that may never be stored, which the probe
// would drop for good.
func TestSink_ABatchAnswered503WithARetryAfterIsKeptAndSentAgainAfterThatLong(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(attempt int, batch []probeproto.Result, w http.ResponseWriter) {
		if attempt == 1 {
			fail(w, http.StatusServiceUnavailable, "a result in this batch is being stored by another request: send the batch again", 5*time.Second)
			return
		}
		// The other request stored the first result, and the second is the probe's to be stored now
		server.mu.Lock()
		server.stored = append(server.stored, batch[1])
		server.mu.Unlock()
		reply(w, http.StatusOK, probeproto.ResultsResponse{Results: []probeproto.ResultOutcome{
			{ID: batch[0].ID, Reason: probeproto.ReasonDuplicate},
			{ID: batch[1].ID, Accepted: true},
		}})
	}
	pause := &sleeper{}
	sink, _ := newSink(server, pause, nil)
	ingest(t, sink,
		checked("m1", 0, shared.MonitorStatusUp, "first"),
		checked("m1", 1, shared.MonitorStatusUp, "second"),
	)

	require.NoError(t, sink.Flush(context.Background()))

	assert.Equal(t, []int{2, 2}, server.batchSizes(), "the same two results are sent again, none was dropped")
	assert.Equal(t, []time.Duration{5 * time.Second}, pause.waits(), "after the Retry-After")
	assert.Equal(t, []string{"hello", "results", "hello", "results"}, server.sequence(), "with a hello in between, as after any 503")
	stored := server.storedResults()
	require.Len(t, stored, 1)
	assert.Equal(t, "second", stored[0].Message)
	assert.Zero(t, sink.Buffered())
	assert.Zero(t, sink.Dropped())
}

func TestSink_FullBufferDropsTheOldest(t *testing.T) {
	server := newResultsServer(t, func(h *probeproto.HelloResponse) { h.BufferMax = 3 })
	logger, logs := observedLogger()
	sink, session := newSink(server, &sleeper{}, func(c *probeclient.SessionConfig) { c.Logger = logger })
	require.NoError(t, session.Connect(context.Background()), "the buffer's size is the one hello states")

	for i := range 5 {
		ingest(t, sink, checked("m1", i, shared.MonitorStatusUp, "result "+string(rune('0'+i))))
	}

	assert.Equal(t, 3, sink.Buffered())
	assert.EqualValues(t, 2, sink.Dropped())
	dropped := logs.FilterMessage("The result buffer is full; dropped the oldest results").All()
	require.NotEmpty(t, dropped, "a drop is logged")
	assert.EqualValues(t, 1, dropped[0].ContextMap()["dropped"])
	assert.EqualValues(t, 3, dropped[0].ContextMap()["buffer_max"])

	require.NoError(t, sink.Flush(context.Background()))

	stored := server.storedResults()
	require.Len(t, stored, 3)
	assert.Equal(t, []string{"result 2", "result 3", "result 4"}, []string{stored[0].Message, stored[1].Message, stored[2].Message},
		"the two oldest are the ones lost")
}

func TestSink_LogDropsLogsTheDropsNotLoggedYet(t *testing.T) {
	const message = "The result buffer is full; dropped the oldest results"
	server := newResultsServer(t, func(h *probeproto.HelloResponse) { h.BufferMax = 2 })
	logger, logs := observedLogger()
	sink, session := newSink(server, &sleeper{}, func(c *probeclient.SessionConfig) { c.Logger = logger })
	require.NoError(t, session.Connect(context.Background()))

	sink.LogDrops()
	assert.Empty(t, logs.FilterMessage(message).All(), "nothing was dropped, so nothing is logged")

	for i := range 5 {
		ingest(t, sink, checked("m1", i, shared.MonitorStatusUp, "ok"))
	}
	require.Len(t, logs.FilterMessage(message).All(), 1, "three results were dropped, and only the first drop was logged as it happened")

	sink.LogDrops()

	dropped := logs.FilterMessage(message).All()
	require.Len(t, dropped, 2)
	assert.EqualValues(t, 2, dropped[1].ContextMap()["dropped"], "the drops since the log before")
	assert.EqualValues(t, 3, dropped[1].ContextMap()["total_dropped"])
	assert.EqualValues(t, 2, dropped[1].ContextMap()["buffer_max"])

	sink.LogDrops()
	assert.Len(t, logs.FilterMessage(message).All(), 2, "and they are logged once")
}

func TestSink_FlushesAtBatchMaxOrInterval(t *testing.T) {
	small := func(h *probeproto.HelloResponse) { h.BatchMax = 3; h.FlushIntervalMs = 2000 }

	t.Run("at batch_max, without waiting out the interval", func(t *testing.T) {
		server := newResultsServer(t, small)
		pause := &sleeper{gate: make(chan struct{})} // the interval never ends
		sink, session := newSink(server, pause, nil)
		require.NoError(t, session.Connect(context.Background()), "the limits are the ones hello states")
		runSink(t, sink)

		ingest(t, sink, checked("m1", 0, shared.MonitorStatusUp, "a"), checked("m1", 1, shared.MonitorStatusUp, "b"))
		require.Eventually(t, func() bool { return len(pause.waits()) == 1 }, 5*time.Second, time.Millisecond)
		assert.Equal(t, []time.Duration{2 * time.Second}, pause.waits(), "it waits flush_interval after the first result")
		assert.Equal(t, []string{"hello"}, server.sequence(), "and two results are not a batch")
		ingest(t, sink, checked("m1", 2, shared.MonitorStatusUp, "c"))

		require.Eventually(t, func() bool { return len(server.storedResults()) == 3 }, 5*time.Second, time.Millisecond)
		assert.Equal(t, []int{3}, server.batchSizes())
	})

	t.Run("at flush_interval after the first result", func(t *testing.T) {
		server := newResultsServer(t, small)
		pause := &sleeper{gate: make(chan struct{}, 1)}
		sink, session := newSink(server, pause, nil)
		require.NoError(t, session.Connect(context.Background()), "the limits are the ones hello states")
		runSink(t, sink)

		ingest(t, sink, checked("m1", 0, shared.MonitorStatusUp, "a"))
		require.Eventually(t, func() bool { return len(pause.waits()) == 1 }, 5*time.Second, time.Millisecond)
		assert.Equal(t, []time.Duration{2 * time.Second}, pause.waits())
		assert.Empty(t, server.storedResults(), "one result waits for the interval")

		pause.gate <- struct{}{} // the interval ends

		require.Eventually(t, func() bool { return len(server.storedResults()) == 1 }, 5*time.Second, time.Millisecond)
		assert.Equal(t, []int{1}, server.batchSizes())
	})

	t.Run("a flush sends what it holds as batches of batch_max, oldest first", func(t *testing.T) {
		server := newResultsServer(t, small)
		sink, session := newSink(server, &sleeper{}, nil)
		require.NoError(t, session.Connect(context.Background()), "the limits are the ones hello states")
		for i := range 7 {
			ingest(t, sink, checked("m1", i, shared.MonitorStatusUp, "result "+string(rune('0'+i))))
		}

		require.NoError(t, sink.Flush(context.Background()))

		assert.Equal(t, []int{3, 3, 1}, server.batchSizes())
		var messages []string
		for _, r := range server.storedResults() {
			messages = append(messages, r.Message)
		}
		assert.Equal(t, []string{"result 0", "result 1", "result 2", "result 3", "result 4", "result 5", "result 6"}, messages)
	})
}

func TestSink_BatchesStayUnderTheBodyLimit(t *testing.T) {
	server := newResultsServer(t, nil)
	sink, _ := newSink(server, &sleeper{}, nil)
	big := strings.Repeat("x", 100<<10)
	for i := range 20 {
		ingest(t, sink, checked("m1", i, shared.MonitorStatusDown, big))
	}

	require.NoError(t, sink.Flush(context.Background()))

	assert.Len(t, server.storedResults(), 20)
	require.Greater(t, len(server.batchSizes()), 1, "twenty results of 100 KiB don't fit one request")
	server.mu.Lock()
	defer server.mu.Unlock()
	for i, size := range server.bodySizes {
		assert.Less(t, size, 1<<20, "batch %d is below the server's limit of 1 MiB", i+1)
	}
}

func TestSink_DropsAResultTooLargeForTheServerToRead(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(_ int, batch []probeproto.Result, w http.ResponseWriter) {
		for _, r := range batch {
			if len(r.Message) > 1<<20 {
				fail(w, http.StatusRequestEntityTooLarge, "request body too large", 0)
				return
			}
		}
		server.accept(w, batch)
	}
	sink, _ := newSink(server, &sleeper{}, nil)
	ingest(t, sink,
		checked("m1", 0, shared.MonitorStatusUp, "before"),
		checked("m1", 1, shared.MonitorStatusDown, strings.Repeat("x", 1<<20+1)),
		checked("m1", 2, shared.MonitorStatusUp, "after"),
	)

	require.NoError(t, sink.Flush(context.Background()))

	var messages []string
	for _, r := range server.storedResults() {
		messages = append(messages, r.Message)
	}
	assert.Equal(t, []string{"before", "after"}, messages, "the result no batch can carry is dropped and does not hold up the rest")
	assert.Zero(t, sink.Buffered())
}

func TestSink_HelloAgainOn409(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(attempt int, batch []probeproto.Result, w http.ResponseWriter) {
		if attempt == 1 {
			fail(w, http.StatusConflict, "unknown instance: send hello first", 0)
			return
		}
		server.accept(w, batch)
	}
	pause := &sleeper{}
	sink, _ := newSink(server, pause, nil)
	ingest(t, sink, checked("m1", 0, shared.MonitorStatusUp, "ok"))

	require.NoError(t, sink.Flush(context.Background()))

	assert.Equal(t, []string{"hello", "results", "hello", "results"}, server.sequence())
	assert.Empty(t, pause.waits())
	assert.Len(t, server.storedResults(), 1)
}

// The first 409 is a server that forgot the instance, which hello puts right at once. A 409 again
// after that hello is a server that keeps forgetting it, which must not be said hello to in a tight loop.
func TestSink_BacksOffOnA409AfterHello(t *testing.T) {
	server := newResultsServer(t, nil)
	server.answer = func(attempt int, batch []probeproto.Result, w http.ResponseWriter) {
		if attempt <= 5 {
			fail(w, http.StatusConflict, "unknown instance: send hello first", 0)
			return
		}
		server.accept(w, batch)
	}
	pause := &sleeper{}
	sink, _ := newSink(server, pause, nil)
	ingest(t, sink, checked("m1", 0, shared.MonitorStatusUp, "ok"))

	require.NoError(t, sink.Flush(context.Background()))

	waits := pause.waits()
	require.Len(t, waits, 4, "the first 409 is followed by no wait, each one after it by a wait")
	for i, step := range []time.Duration{1, 2, 4, 8} {
		assert.GreaterOrEqual(t, waits[i], step*time.Second/2, "wait %d", i+1)
		assert.LessOrEqual(t, waits[i], step*time.Second, "wait %d: the backoff starts at its minimum", i+1)
	}
	assert.Equal(t, []string{"hello", "results", "hello", "results", "hello", "results", "hello", "results", "hello", "results", "hello", "results"}, server.sequence(),
		"every 409 is followed by a hello")
	assert.Len(t, server.storedResults(), 1)
}

func TestSink_IngestDoesNotWaitForTheServer(t *testing.T) {
	hung := make(chan struct{})
	server := newResultsServer(t, nil)
	server.answer = func(_ int, _ []probeproto.Result, _ http.ResponseWriter) { <-hung }
	t.Cleanup(func() { close(hung) })
	intervalOver := make(chan struct{})
	close(intervalOver)
	sink, _ := newSink(server, &sleeper{gate: intervalOver}, nil)
	runSink(t, sink)
	ingest(t, sink, checked("m1", 0, shared.MonitorStatusUp, "ok"))
	require.Eventually(t, func() bool { return server.attemptCount() == 1 }, 5*time.Second, time.Millisecond, "a batch is on the wire, and the server holds it")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			_ = sink.Ingest(context.Background(), checked("m1", i+1, shared.MonitorStatusUp, "ok"))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		require.Fail(t, "Ingest waited for the server")
	}
}

func TestSink_SendsTheResultAsTheServerReadsIt(t *testing.T) {
	server := newResultsServer(t, nil)
	sink, _ := newSink(server, &sleeper{}, func(c *probeclient.SessionConfig) {
		c.Now = func() time.Time { return time.UnixMilli(1469918176385) } // the instant of the ULID specification's example
	})
	start := time.Date(2026, 10, 23, 11, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	validTo := time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)
	ingest(t, sink,
		&ingester.Result{
			MonitorID: "m1", Status: shared.MonitorStatusUp, Message: "200 - OK", PingMs: 87, StartTime: start, EndTime: start.Add(87 * time.Millisecond),
			TLSInfo: &certificate.TLSInfo{Valid: true, CertInfo: &certificate.CertificateInfo{
				Subject: "CN=example.com", Issuer: "CN=Example CA", DaysRemaining: 73, ValidTo: validTo, CertType: "server",
			}},
		},
		&ingester.Result{
			MonitorID: "m2", Status: shared.MonitorStatusDown, Message: "connection refused", StartTime: start, EndTime: start,
		},
		&ingester.Result{
			MonitorID: "m3", Status: shared.MonitorStatusPending, Message: "retrying", StartTime: start, EndTime: start,
		},
		&ingester.Result{
			MonitorID: "m4", Status: shared.MonitorStatusMaintenance, Message: "maintenance", StartTime: start, EndTime: start,
		},
	)

	require.NoError(t, sink.Flush(context.Background()))

	stored := server.storedResults()
	require.Len(t, stored, 4)
	assert.Equal(t, []string{"up", "down", "pending", "maintenance"}, []string{stored[0].Status, stored[1].Status, stored[2].Status, stored[3].Status})
	assert.Equal(t, "m1", stored[0].MonitorID)
	assert.Equal(t, "200 - OK", stored[0].Message)
	assert.Equal(t, 87, stored[0].PingMs)
	assert.True(t, stored[0].StartTime.Equal(start), "the start is the instant the check started")
	assert.True(t, stored[0].EndTime.Equal(start.Add(87*time.Millisecond)))
	assert.JSONEq(t, `{
		"valid": true,
		"certInfo": {
			"subject": "CN=example.com", "issuer": "CN=Example CA", "fingerprint": "", "fingerprint256": "", "serialNumber": "",
			"validFrom": "0001-01-01T00:00:00Z", "validTo": "2027-01-05T00:00:00Z", "daysRemaining": 73, "certType": "server",
			"signatureAlgorithm": "", "publicKeyAlgorithm": "", "version": 0
		}
	}`, string(stored[0].TLSInfo), "the certificate is in the shape the server decodes")
	assert.Empty(t, stored[1].TLSInfo, "a check that read no certificate sends none")

	ulid := regexp.MustCompile(`^01ARYZ6S41[0-9A-HJKMNP-TV-Z]{16}$`)
	for i, r := range stored {
		assert.Regexp(t, ulid, r.ID, "result %d: a ULID made at 1469918176385 ms", i)
	}
	_, ids := withoutIDs(stored)
	assert.IsIncreasing(t, ids, "ULIDs made in the same millisecond still go in order")
}

func TestSink_RejectsAStatusTheServerDoesNotKnow(t *testing.T) {
	server := newResultsServer(t, nil)
	sink, _ := newSink(server, &sleeper{}, nil)

	err := sink.Ingest(context.Background(), checked("m1", 0, shared.MonitorStatus(42), "?"))

	require.Error(t, err)
	assert.Zero(t, sink.Buffered())
}

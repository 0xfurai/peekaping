package probeclient_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/probeclient"
	"peekaping/internal/probeproto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httpTarget is a web server a monitor checks, and counts the checks
func httpTarget(t *testing.T) (url string, checks *atomic.Int32) {
	t.Helper()
	checks = new(atomic.Int32)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	return target.URL, checks
}

// checkedOnce is a check interval so long that a monitor is checked at once, when the probe gets it,
// and not again while a test runs. The scheduler checks a monitor on each multiple of its interval
// counted from the Unix epoch, so with an interval of a minute a test that began less than a flush
// interval (2 s) before a full minute got a second result from the tick on that minute, and a test
// that waits for exactly one result never saw it. The first multiple of this one is in 2038.
const checkedOnce = math.MaxInt32

// monitorOf is an HTTP monitor of the target, which is checked once
func monitorOf(id, url string) probeproto.Monitor {
	return httpMonitor(id, func(m *probeproto.Monitor) {
		m.Interval = checkedOnce
		m.Config = fmt.Sprintf(`{"url":%q,"method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`, url)
	})
}

func newProbe(server *assignmentServer) *probeclient.Probe {
	return probeclient.New(probeclient.Config{URL: server.URL, Token: probeToken, Version: "0.0.48", Logger: nopLogger(), FlushTimeout: 2 * time.Second})
}

// runProbe runs the probe until the test ends
func runProbe(t *testing.T, probe *probeclient.Probe) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, probe.Run(ctx))
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = probe.Shutdown(shutdown)
	})
}

func TestProbe_ChecksItsAssignmentAndSendsTheResults(t *testing.T) {
	url, checks := httpTarget(t)
	server := newAssignmentServer(t, snapshot("v1", monitorOf("a", url)))
	probe := newProbe(server)
	assert.False(t, probe.Ready(), "no assignment yet")

	runProbe(t, probe)

	require.Eventually(t, probe.Ready, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return checks.Load() == 1 }, 5*time.Second, time.Millisecond, "a new monitor is checked at once")
	require.Eventually(t, func() bool { return len(server.storedResults()) == 1 }, 10*time.Second, 10*time.Millisecond, "and its result is sent")
	result := server.storedResults()[0]
	assert.Equal(t, "a", result.MonitorID)
	assert.Equal(t, probeproto.StatusUp, result.Status)
}

func TestProbe_ShutdownLetsTheCheckFinishFlushesAndSaysBye(t *testing.T) {
	slow := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-slow
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	server := newAssignmentServer(t, snapshot("v1", monitorOf("a", target.URL)))
	probe := newProbe(server)
	runProbe(t, probe)
	require.Eventually(t, probe.Ready, 5*time.Second, time.Millisecond)
	time.Sleep(100 * time.Millisecond) // the check is under way, held by the target

	finished := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		finished <- probe.Shutdown(ctx)
	}()
	select {
	case <-finished:
		require.Fail(t, "Shutdown did not wait for the running check")
	case <-time.After(300 * time.Millisecond):
	}
	close(slow)

	require.NoError(t, <-finished)
	stored := server.storedResults()
	require.Len(t, stored, 1, "the check that was running is sent before Shutdown returns, not left to the flush interval")
	assert.Equal(t, probeproto.StatusUp, stored[0].Status)
	events := server.sequence()
	require.NotEmpty(t, events)
	assert.Equal(t, "bye", events[len(events)-1], "and the last thing the probe says is bye")
	assert.Equal(t, "results", events[len(events)-2])
}

// A probe in a container is watched through its log, so a shutdown tells each step: the checks
// have finished, the held results are sent, and the server has been told.
func TestProbe_ShutdownLogsTheDrainTheFlushAndTheBye(t *testing.T) {
	slow := make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-slow
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	server := newAssignmentServer(t, snapshot("v1", monitorOf("a", target.URL)))
	logger, logs := observedLogger()
	probe := probeclient.New(probeclient.Config{URL: server.URL, Token: probeToken, Version: "0.0.48", Logger: logger, FlushTimeout: 2 * time.Second})
	runProbe(t, probe)
	require.Eventually(t, probe.Ready, 5*time.Second, time.Millisecond)
	time.Sleep(100 * time.Millisecond) // the check is under way, held by the target
	close(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, probe.Shutdown(ctx))

	var steps []string
	for _, entry := range logs.All() {
		switch entry.Message {
		case "Scheduler stopped", "Flushed the buffered results", "Told the server this instance is stopping", "Probe stopped":
			steps = append(steps, entry.Message)
		}
	}
	assert.Equal(t, []string{
		"Scheduler stopped",
		"Flushed the buffered results",
		"Told the server this instance is stopping",
		"Probe stopped",
	}, steps)
	flushed := logs.FilterMessage("Flushed the buffered results").All()
	require.Len(t, flushed, 1)
	assert.EqualValues(t, 1, flushed[0].ContextMap()["buffered"], "the result of the check that was running")
}

// Drops are logged once in a while as they happen, and the ones after the last such log would
// never be logged at all, so the shutdown logs them.
func TestProbe_ShutdownLogsTheDropsNotLoggedYet(t *testing.T) {
	url, checks := httpTarget(t)
	server := newAssignmentServerWith(t,
		func(h *probeproto.HelloResponse) { h.BufferMax = 1; h.FlushIntervalMs = 60000 },
		snapshot("v1", monitorOf("a", url), monitorOf("b", url), monitorOf("c", url)))
	logger, logs := observedLogger()
	probe := probeclient.New(probeclient.Config{URL: server.URL, Token: probeToken, Version: "0.0.48", Logger: logger, FlushTimeout: 2 * time.Second})
	runProbe(t, probe)
	require.Eventually(t, func() bool { return checks.Load() == 3 }, 5*time.Second, time.Millisecond, "the three monitors are checked at once")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, probe.Shutdown(ctx))

	dropped := logs.FilterMessage("The result buffer is full; dropped the oldest results").All()
	require.Len(t, dropped, 2, "the first drop is logged as it happens, and the second when the probe stops")
	assert.EqualValues(t, 1, dropped[0].ContextMap()["dropped"])
	assert.EqualValues(t, 1, dropped[1].ContextMap()["dropped"], "the drops since the log before")
	assert.EqualValues(t, 2, dropped[1].ContextMap()["total_dropped"])
	assert.EqualValues(t, 1, dropped[1].ContextMap()["buffer_max"])
}

// A container is stopped on a clock (35 s in the compose files, 30 s in Kubernetes), so Shutdown has
// to be over by the deadline it is given, however long a check runs. The checks get the time before
// the flush and the bye's share of it, and the last results are sent and the bye said in the rest.
func TestProbe_ShutdownEndsWithinItsDeadlineWhenACheckNeverEnds(t *testing.T) {
	release := make(chan struct{})
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(stuck.Close)
	t.Cleanup(func() { close(release) })
	url, checks := httpTarget(t)
	server := newAssignmentServer(t, snapshot("v1", monitorOf("stuck", stuck.URL), monitorOf("quick", url)))
	probe := newProbe(server)
	runProbe(t, probe)
	require.Eventually(t, func() bool { return checks.Load() == 1 }, 5*time.Second, time.Millisecond, "the quick monitor is checked at once, and the stuck one never ends")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := probe.Shutdown(ctx)

	assert.NoError(t, ctx.Err(), "Shutdown returned before its deadline")
	assert.NoError(t, err)
	var monitors []string
	for _, result := range server.storedResults() {
		monitors = append(monitors, result.MonitorID)
	}
	assert.Contains(t, monitors, "quick", "the result that was held is sent")
	events := server.sequence()
	require.NotEmpty(t, events)
	assert.Equal(t, "bye", events[len(events)-1], "and the last thing the probe says is bye")
}

// The server being unreachable is the case a probe is built for, and it must not make Shutdown
// overstay either: a batch the server holds without an answer is given up with the flush.
func TestProbe_ShutdownEndsWithinItsDeadlineWhenTheServerStopsAnswering(t *testing.T) {
	release := make(chan struct{})
	var posted atomic.Int32
	url, _ := httpTarget(t)
	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/probe/v1/hello":
			welcome := welcomeFrom(time.Now())
			welcome.FlushIntervalMs = 10
			reply(w, http.StatusOK, welcome)
		case "/probe/v1/assignments":
			if r.URL.Query().Get("version") == "" {
				reply(w, http.StatusOK, probeproto.AssignmentsResponse{Version: "v1", Monitors: []probeproto.Monitor{monitorOf("a", url)}})
				return
			}
			<-r.Context().Done()
		case "/probe/v1/results":
			posted.Add(1)
			select { // the batch is never answered
			case <-release:
			case <-r.Context().Done():
			}
		case "/probe/v1/bye":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	t.Cleanup(func() { close(release) })
	probe := probeclient.New(probeclient.Config{URL: server.URL, Token: probeToken, Version: "0.0.48", Logger: nopLogger()})
	runProbe(t, probe)
	require.Eventually(t, func() bool { return posted.Load() >= 1 }, 5*time.Second, time.Millisecond, "the flusher is waiting for the server's answer")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- probe.Shutdown(ctx) }()
	var err error
	select {
	case err = <-finished:
	case <-time.After(10 * time.Second):
		require.Fail(t, "Shutdown did not end")
	}

	assert.NoError(t, ctx.Err(), "Shutdown returned before its deadline")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the results that could not be sent are reported")
}

// Without a deadline, Shutdown waits for the checks for as long as ctx lasts; cancelling ctx then
// cancels the checks, and the results are still sent and the bye said.
func TestProbe_ShutdownWithoutADeadlineWaitsForTheChecksUntilCancelled(t *testing.T) {
	release := make(chan struct{})
	stuck := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(stuck.Close)
	t.Cleanup(func() { close(release) })
	url, checks := httpTarget(t)
	server := newAssignmentServer(t, snapshot("v1", monitorOf("stuck", stuck.URL), monitorOf("quick", url)))
	probe := newProbe(server)
	runProbe(t, probe)
	require.Eventually(t, func() bool { return checks.Load() == 1 }, 5*time.Second, time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- probe.Shutdown(ctx) }()
	select {
	case <-finished:
		require.Fail(t, "Shutdown did not wait for the check that never ends")
	case <-time.After(500 * time.Millisecond):
	}
	cancel()

	select {
	case err := <-finished:
		assert.NoError(t, err)
	case <-time.After(10 * time.Second):
		require.Fail(t, "Shutdown did not end when ctx was cancelled")
	}
	var monitors []string
	for _, result := range server.storedResults() {
		monitors = append(monitors, result.MonitorID)
	}
	assert.Contains(t, monitors, "quick")
	events := server.sequence()
	require.NotEmpty(t, events)
	assert.Equal(t, "bye", events[len(events)-1])
}

func TestProbe_KeepsCheckingWhileTheServerIsUnreachable(t *testing.T) {
	url, checks := httpTarget(t)
	server := newAssignmentServer(t, snapshot("v1", httpMonitor("a", func(m *probeproto.Monitor) {
		m.Interval = 1
		m.Config = monitorOf("a", url).Config
	})))
	pause := &sleeper{after: func(int, time.Duration) { time.Sleep(10 * time.Millisecond) }}
	probe := probeclient.New(probeclient.Config{URL: server.URL, Token: probeToken, Logger: nopLogger(), Sleep: pause.Sleep, FlushTimeout: 200 * time.Millisecond})
	runProbe(t, probe)
	require.Eventually(t, func() bool { return checks.Load() >= 1 }, 5*time.Second, time.Millisecond)

	server.CloseClientConnections()
	server.Close()
	before := checks.Load()

	require.Eventually(t, func() bool { return checks.Load() >= before+2 }, 10*time.Second, 10*time.Millisecond,
		"the probe goes on checking its last assignment")
	assert.True(t, probe.Ready(), "and stays ready")
}

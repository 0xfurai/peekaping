package probeclient_test

import (
	"context"
	"fmt"
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

// monitorOf is an HTTP monitor of the target
func monitorOf(id, url string) probeproto.Monitor {
	return httpMonitor(id, func(m *probeproto.Monitor) {
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

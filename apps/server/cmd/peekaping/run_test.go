package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"
	graceful "peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest/observer"
)

const ignoringRedis = "Single mode uses no Redis, ignoring the Redis settings"

func TestRun_ChecksAMonitorCreatedThroughTheAPIAndIngestsPushesWithoutRedis(t *testing.T) {
	// A Redis server that counts the connections it gets
	redisHost, redisPort, redisConnections := countingListener(t)
	cfg := loadConfig(t, map[string]string{"REDIS_HOST": redisHost, "REDIS_PORT": redisPort})
	logger, logs := observedLogger()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(target.Close)

	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	socket := in.dialSocket(t)
	socket.join(t, "monitor:all")

	// The join is handled in its own goroutine. Push until a push reaches the websocket.
	pushID := in.createMonitor(t, pushMonitor("Backup", "backup-push-token"))
	deadline := time.Now().Add(wait)
	for i := 0; ; i++ {
		msg := fmt.Sprintf("push %d", i)
		status, body := in.request(t, http.MethodGet, "/api/v1/push/backup-push-token?msg="+url.QueryEscape(msg), nil)
		require.Equal(t, http.StatusOK, status, string(body))
		if _, ok := socket.until(t, 200*time.Millisecond, func(e socketEvent) bool { return e.hb.MonitorID == pushID && e.hb.Msg == msg }); ok {
			break
		}
		require.True(t, time.Now().Before(deadline), "no push reached the websocket within %s", wait)
	}

	t.Run("a monitor created through the API is checked, and its heartbeat stored and pushed", func(t *testing.T) {
		monitorID := in.createMonitor(t, httpMonitor("Target", target.URL, 20))

		pushed, ok := socket.until(t, wait, func(e socketEvent) bool { return e.hb.MonitorID == monitorID })
		require.True(t, ok, "the check's heartbeat reached the websocket within %s", wait)
		assert.Equal(t, "monitor:all:heartbeat", pushed.name)
		assert.Equal(t, shared.MonitorStatusUp, pushed.hb.Status)
		assert.Equal(t, "200 - 200 OK", pushed.hb.Msg)

		stored := in.heartbeats(t, monitorID)
		assert.True(t, slices.ContainsFunc(stored, func(hb heartbeat.Model) bool { return hb.ID == pushed.hb.ID }),
			"the heartbeat pushed is stored: %v", stored)
	})

	t.Run("a push to the push URL is stored before the push URL answers", func(t *testing.T) {
		status, body := in.request(t, http.MethodGet, "/api/v1/push/backup-push-token?status=down&msg=disk%20full&ping=12", nil)
		require.Equal(t, http.StatusOK, status, string(body))

		stored := in.heartbeats(t, pushID)
		i := slices.IndexFunc(stored, func(hb heartbeat.Model) bool { return hb.Msg == "disk full" })
		require.GreaterOrEqual(t, i, 0, "the push is stored: %v", stored)
		assert.Equal(t, shared.MonitorStatusDown, stored[i].Status)
		assert.Equal(t, 12, stored[i].Ping)
	})

	require.NoError(t, in.stop(t))

	assert.Zero(t, redisConnections(), "peekaping never connects to Redis")
	ignored := logs.FilterMessage(ignoringRedis).All()
	require.Len(t, ignored, 1, "it says once that it ignores the Redis settings")
	assert.Equal(t, []any{"REDIS_HOST", "REDIS_PORT"}, ignored[0].ContextMap()["settings"])
}

func TestRun_ShutsDownInOrderOnceTheRequestsInFlightAreAnswered(t *testing.T) {
	// With the metrics collector too, whose sync the shutdown waits for
	cfg := loadConfig(t, map[string]string{"METRICS_ENABLED": "true"})
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	// A login whose body is still on its way when the shutdown starts
	conn, rest := in.sendHalfALogin(t)

	startedAt := time.Now()
	stopped := make(chan error, 1)
	go func() { stopped <- in.stop(t) }()

	time.Sleep(500 * time.Millisecond)
	select {
	case err := <-stopped:
		require.FailNow(t, "run returned with a request in flight", "%v", err)
	default:
	}
	assert.Empty(t, logs.FilterMessage("Stopped the HTTP server").All(), "the HTTP server waits for the request in flight")

	_, err := io.WriteString(conn, rest)
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the login in flight is answered")

	select {
	case err := <-stopped:
		require.NoError(t, err, "run returns nil, so peekaping exits 0")
	case <-time.After(shutdownTimeouts.Total()):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	t.Logf("shut down in %s", time.Since(startedAt).Round(time.Millisecond))

	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
	assert.Empty(t, logs.FilterMessage(ignoringRedis).All(), "no REDIS_* variable is set")
}

func TestRun_ShutsDownWithinSecondsWhileAWebsocketClientLongPolls(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	// Its reader keeps a long poll open, which the server answers at the latest with the next
	// ping, 25 seconds away: past Docker's default stop grace of 10 seconds
	socket := in.dialSocket(t)
	time.Sleep(200 * time.Millisecond) // the reader sends its next long poll

	startedAt := time.Now()
	require.NoError(t, in.stop(t), "run returns nil, so peekaping exits 0")
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	assert.Less(t, took, 5*time.Second, "the long poll doesn't hold up the shutdown")
	select {
	case _, open := <-socket.packets:
		assert.False(t, open, "the websocket server ended the client's session")
	case <-time.After(wait):
		assert.Fail(t, "the client's session is still open")
	}
	// The websocket closes from the HTTP server's Shutdown, once it takes no new connections, and
	// is logged once the HTTP server has stopped: the order of ADR 0002, section 3
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

func TestRun_ShutsDownWithinSecondsWhileAWebsocketClientIsConnected(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	// Its connection is hijacked from the HTTP server, which doesn't wait for it
	conn := in.dialWebsocket(t)

	startedAt := time.Now()
	require.NoError(t, in.stop(t), "run returns nil, so peekaping exits 0")
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	assert.Less(t, took, 5*time.Second, "the websocket doesn't hold up the shutdown")
	// The websocket server closes the client's session and its connection
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wait)))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			var netErr net.Error
			assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "the connection is still open: %v", err)
			break
		}
		t.Logf("read %q", msg)
	}
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

// ingestWaitingForTheDatabase checks a new monitor, whose result's ingest then waits for the
// database's write lock, held by another connection until unlock is called. The scheduler stops
// only once that ingest is done.
func (in *instance) ingestWaitingForTheDatabase(t *testing.T, cfg *Config) (monitorID string, unlock func()) {
	t.Helper()
	reached, answer := make(chan struct{}, 1), make(chan struct{})
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		select {
		case <-answer:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(target.Close)

	monitorID = in.createMonitor(t, httpMonitor("Target", target.URL, 20))
	select {
	case <-reached:
	case <-time.After(wait):
		require.FailNow(t, "the monitor wasn't checked within "+wait.String())
	}
	unlock = lockDatabase(t, cfg.DBName)
	close(answer)
	time.Sleep(500 * time.Millisecond) // the ingest gets to its first write
	return monitorID, unlock
}

func TestRun_ClosesTheDatabaseOnlyOnceTheSchedulerHasStoppedEvenAfterTheHTTPServerRanLate(t *testing.T) {
	// A request in flight uses up the HTTP server's share of the shutdown
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	monitorID, unlock := in.ingestWaitingForTheDatabase(t, cfg)
	in.sendHalfALogin(t)

	stopped := make(chan error, 1)
	go func() { stopped <- in.stop(t) }()
	require.Eventually(t, func() bool { return logs.FilterMessage("Stopped the HTTP server").Len() == 1 },
		wait, 10*time.Millisecond, "the HTTP server's share runs out")
	time.Sleep(500 * time.Millisecond)
	assert.Zero(t, logs.FilterMessage("Scheduler stopped").Len(), "the scheduler waits for the ingest under way")
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database stays open meanwhile")

	unlock()
	var err error
	select {
	case err = <-stopped:
	case <-time.After(shutdownTimeouts.Total()):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	require.Error(t, err, "the HTTP server didn't stop in time")
	assert.Contains(t, err.Error(), "failed to shut down the HTTP server")

	order := []string{
		"Stopped the HTTP server",
		"Closed the websocket server",
		"Scheduler stopped",
		"Stopped the scheduler, the watchdog and the other background jobs",
		"Closed the event bus",
		"Performing graceful SQLite shutdown...",
		"Closed the database",
	}
	assert.Equal(t, order, logged(logs, order), "the shutdown's steps, in the order logged")
	assert.Zero(t, logs.FilterMessage("Failed to ingest the result of a check").Len())
	assert.Equal(t, 1, countHeartbeats(t, cfg.DBName, monitorID), "the check's result is stored")
}

func TestRun_LeavesTheDatabaseOpenWhenTheSchedulerDoesntStopInTime(t *testing.T) {
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.Jobs = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	monitorID, unlock := in.ingestWaitingForTheDatabase(t, cfg)

	err := in.stop(t)
	require.Error(t, err, "peekaping exits 1")
	assert.Contains(t, err.Error(), "the scheduler didn't stop in time")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"background jobs"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database isn't closed under the ingest")

	// The ingest finishes on the database left open
	unlock()
	require.Eventually(t, func() bool { return logs.FilterMessage("Scheduler stopped").Len() == 1 }, wait, 10*time.Millisecond)
	assert.Zero(t, logs.FilterMessage("Failed to ingest the result of a check").Len())
	assert.Equal(t, 1, countHeartbeats(t, cfg.DBName, monitorID), "the check's result is stored")
}

// createWaitingForTheDatabase sends the creation of a monitor, whose handler then waits for the
// database's write lock, held by another connection until unlock is called. The HTTP server's
// Close drops the request's connection, which cancels the handler's context but doesn't stop its
// wait: once unlocked, the handler fails, logging "Failed to create monitor".
func (in *instance) createWaitingForTheDatabase(t *testing.T, cfg *Config) (unlock func()) {
	t.Helper()
	body, err := json.Marshal(pushMonitor("Waiting", "waiting-push-token"))
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, in.url+"/api/v1/monitors", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+in.accessToken)

	unlock = lockDatabase(t, cfg.DBName)
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	t.Cleanup(func() { <-answered })
	time.Sleep(500 * time.Millisecond) // the handler gets to its write
	return unlock
}

func TestRun_ClosesTheDatabaseOnlyOnceAnHTTPHandlerTheHTTPServerLeftRunningHasReturned(t *testing.T) {
	// The handler outlives the HTTP server's share
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	unlock := in.createWaitingForTheDatabase(t, cfg)

	stopped := make(chan error, 1)
	go func() { stopped <- in.stop(t) }()
	require.Eventually(t, func() bool { return logs.FilterMessage("Stopped the HTTP server").Len() == 1 },
		wait, 10*time.Millisecond, "the HTTP server's share runs out")
	time.Sleep(500 * time.Millisecond)
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database stays open while the handler waits")

	unlock()
	var err error
	select {
	case err = <-stopped:
	case <-time.After(shutdownTimeouts.Total()):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	require.Error(t, err, "the HTTP server didn't stop in time")
	assert.Contains(t, err.Error(), "failed to shut down the HTTP server")
	assert.NotContains(t, err.Error(), "the HTTP handlers", "the handler returned in time")

	order := []string{
		"Stopped the HTTP server",
		"Closed the websocket server",
		"Stopped the scheduler, the watchdog and the other background jobs",
		"Failed to create monitor",
		"Closed the event bus",
		"Performing graceful SQLite shutdown...",
		"Closed the database",
	}
	assert.Equal(t, order, logged(logs, order), "the handler returns before the event bus and the database close")
	failed := logs.FilterMessage("Failed to create monitor").All()
	require.Len(t, failed, 1)
	assert.Equal(t, "context canceled", failed[0].ContextMap()["error"],
		"the handler fails since its client is gone, not since the database closed")
}

func TestRun_LeavesTheDatabaseOpenWhenAnHTTPHandlerDoesntReturnInTime(t *testing.T) {
	// The handler outlives the HTTP server's share and the background jobs' share
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	shutdownTimeouts.Jobs = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)
	unlock := in.createWaitingForTheDatabase(t, cfg)

	err := in.stop(t)
	require.Error(t, err, "peekaping exits 1")
	assert.Contains(t, err.Error(), "failed to shut down the HTTP server")
	assert.Contains(t, err.Error(), "the HTTP handlers didn't stop in time")
	left := logs.FilterMessage("Left the database open, since some of what uses it is still running").All()
	require.Len(t, left, 1)
	assert.Equal(t, []any{"HTTP handlers"}, left[0].ContextMap()["running"])
	assert.Zero(t, logs.FilterMessage("Performing graceful SQLite shutdown...").Len(), "the database isn't closed under the handler")

	assert.Zero(t, logs.FilterMessage("Failed to create monitor").Len(), "the handler still waits")

	// The handler finishes on the database left open
	unlock()
	require.Eventually(t, func() bool { return logs.FilterMessage("Failed to create monitor").Len() == 1 },
		wait, 10*time.Millisecond, "the handler returns")
	assert.Equal(t, "context canceled", logs.FilterMessage("Failed to create monitor").All()[0].ContextMap()["error"],
		"the handler fails since its client is gone, not since the database closed")
}

// shutdownOrder is what a shutdown that stops everything in time logs, in this order
var shutdownOrder = []string{
	"Shutdown signal received, shutting down",
	"Stopped the HTTP server",
	"Closed the websocket server",
	"Stale-monitor watchdog stopped",
	"Probe sweep stopped",
	"Scheduler stopped",
	"Stopped the scheduler, the watchdog and the other background jobs",
	"Closed the event bus",
	"SQLite database closed gracefully",
	"Closed the database",
	"Peekaping stopped",
}

// logged returns the messages logged that are among messages, in the order they were logged
func logged(logs *observer.ObservedLogs, messages []string) []string {
	var got []string
	for _, entry := range logs.All() {
		if slices.Contains(messages, entry.Message) {
			got = append(got, entry.Message)
		}
	}
	return got
}

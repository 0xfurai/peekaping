package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"

	"peekaping/internal/leader"
	graceful "peekaping/internal/shutdown"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestListenAddress(t *testing.T) {
	assert.Equal(t, ":8034", listenAddress("8034"))
	assert.Equal(t, ":9000", listenAddress(":9000"))
	assert.Equal(t, ":8084", listenAddress(""), "the port when none is set")
}

func TestRun_ServesTheAPIUntilItsContextIsDone(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)

	resp, err := http.Get(in.url + "/api/v1/version")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.NoError(t, in.stop(t), "run returns nil, so the API exits 0")
}

// On SQLite and PostgreSQL the api runs the stale-monitor watchdog and the probe sweep, on the
// replica that holds the leader lock. It holds the lock while it runs, and releases
// it on shutdown, before it stops anything else, so that another replica takes over.
func TestRun_HoldsTheLeaderLockWhileRunningAndReleasesItWhenItStops(t *testing.T) {
	cfg := loadConfig(t, nil)
	rdb := redis.NewClient(&redis.Options{Addr: net.JoinHostPort(cfg.RedisHost, cfg.RedisPort)})
	t.Cleanup(func() { _ = rdb.Close() })
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)

	require.Eventually(t, func() bool {
		holder, err := rdb.Get(context.Background(), leader.Key).Result()
		return err == nil && holder != ""
	}, wait, 20*time.Millisecond, "the only replica becomes the leader")
	require.Eventually(t, func() bool { return logs.FilterMessage("Starting the stale-monitor watchdog as leader").Len() == 1 },
		wait, 20*time.Millisecond, "and runs the watchdog")
	assert.Equal(t, 1, logs.FilterMessage("Starting the probe sweep as leader").Len(), "and the probe sweep")

	require.NoError(t, in.stop(t))

	_, err := rdb.Get(context.Background(), leader.Key).Result()
	assert.ErrorIs(t, err, redis.Nil, "the lock is released once the jobs stopped")
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the leader jobs stop right after the websocket closes")
}

// A cluster on SQLite or PostgreSQL that was upgraded without deploying probes has monitors and
// nothing that checks them. The api says so at startup, whether or not it is the leader.
func TestRun_WarnsAtStartupOfALocalProbeWithMonitorsAndNoInstance(t *testing.T) {
	const warning = "the local probe has 1 active monitors and no instance: deploy peekaping-probe with PROBE_TOKEN set to PROBE_BOOTSTRAP_TOKEN"
	cfg := loadConfig(t, nil)

	logger, logs := observedLogger()
	first := startAPI(t, cfg, logger, logs)
	assert.Zero(t, logs.FilterMessage(warning).Len(), "a new install has no monitors to leave without a probe")
	first.registerAdmin(t)
	status, body := first.request(t, http.MethodPost, "/api/v1/monitors", map[string]any{
		"type": "http", "name": "Home", "interval": 60, "retry_interval": 60, "timeout": 16, "max_retries": 3,
		"active": true, "notification_ids": []string{},
		"config": `{"url":"https://example.com/health","method":"GET","encoding":"json","accepted_statuscodes":["2XX"],"authMethod":"none"}`,
	})
	require.Equal(t, http.StatusCreated, status, string(body))
	require.NoError(t, first.stop(t))

	logger, logs = observedLogger()
	startAPI(t, cfg, logger, logs)

	warnings := logs.FilterMessage(warning).All()
	require.Len(t, warnings, 1)
	assert.Equal(t, zap.WarnLevel, warnings[0].Level)
}

func TestRun_ShutsDownInOrderOnceTheRequestsInFlightAreAnswered(t *testing.T) {
	// With the metrics collector too, whose sync the shutdown waits for
	cfg := loadConfig(t, map[string]string{"METRICS_ENABLED": "true"})
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
	in.registerAdmin(t)

	// A login whose body is still on its way when the shutdown starts
	conn, rest := in.sendHalfALogin(t)

	startedAt := time.Now()
	stopped := make(chan error, 1)
	go func() { stopped <- in.stop(t) }()

	// Longer than the time a shutdown takes that doesn't wait for the request
	time.Sleep(1500 * time.Millisecond)
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
		require.NoError(t, err, "run returns nil, so the API exits 0")
	case <-time.After(shutdownTimeouts.Total()):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	t.Logf("shut down in %s", time.Since(startedAt).Round(time.Millisecond))

	assert.Equal(t, shutdownOrderWithMetrics, logged(logs, shutdownOrderWithMetrics), "the shutdown's steps, in the order logged")
}

func TestRun_ShutsDownWithinSecondsWhileAWebsocketClientLongPolls(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
	in.registerAdmin(t)
	// Its reader keeps a long poll open, which the server answers at the latest with the next
	// ping, 25 seconds away: past Docker's default stop grace of 10 seconds
	socket := in.dialSocket(t)
	time.Sleep(200 * time.Millisecond) // the reader sends its next long poll

	startedAt := time.Now()
	require.NoError(t, in.stop(t), "run returns nil, so the API exits 0")
	took := time.Since(startedAt)
	t.Logf("shut down in %s", took.Round(time.Millisecond))

	assert.Less(t, took, 5*time.Second, "the long poll doesn't hold up the shutdown")
	select {
	case _, open := <-socket.packets:
		assert.False(t, open, "the websocket server ended the client's session")
	case <-time.After(wait):
		assert.Fail(t, "the client's session is still open")
	}
	assert.Equal(t, shutdownOrder, logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

func TestRun_ShutsDownWithinSecondsWhileAWebsocketClientIsConnected(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
	in.registerAdmin(t)
	// Its connection is hijacked from the HTTP server, which doesn't wait for it
	conn := in.dialWebsocket(t)

	startedAt := time.Now()
	require.NoError(t, in.stop(t), "run returns nil, so the API exits 0")
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

func TestRun_FailsButStopsEverythingElseWhenARequestOutlastsTheHTTPServersShare(t *testing.T) {
	// A request in flight uses up the HTTP server's share of the shutdown
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
	in.registerAdmin(t)
	in.sendHalfALogin(t)

	err := in.stop(t)

	require.Error(t, err, "the HTTP server didn't stop in time, so the API exits 1")
	assert.Contains(t, err.Error(), "failed to shut down the HTTP server")
	// The request's connection was dropped, and the rest stopped in its own share of the time
	assert.Equal(t, shutdownOrder[:len(shutdownOrder)-1], logged(logs, shutdownOrder), "the shutdown's steps, in the order logged")
}

func TestRun_ClosesTheDatabaseOnlyOnceAnHTTPHandlerTheHTTPServerLeftRunningHasReturned(t *testing.T) {
	// The handler outlives the HTTP server's share
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
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

	// The handler fails once the test unlocks the database, which may be before or after the
	// background jobs stop (the notification queue takes about a second), so only what is
	// ordered is listed
	order := []string{
		"Stopped the HTTP server",
		"Closed the websocket server",
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
	// The handler outlives the HTTP server's share and the background jobs' share, which is
	// longer than the notification queue takes to stop (asynq waits a second for its workers)
	defer func(saved graceful.Timeouts) { shutdownTimeouts = saved }(shutdownTimeouts)
	shutdownTimeouts.HTTP = 300 * time.Millisecond
	shutdownTimeouts.Jobs = 3 * time.Second
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startAPI(t, cfg, logger, logs)
	in.registerAdmin(t)
	unlock := in.createWaitingForTheDatabase(t, cfg)

	err := in.stop(t)
	require.Error(t, err, "the API exits 1")
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

// shutdownOrder is what a shutdown that stops everything in time logs, in this order, with the
// metrics collector off as it is by default
var shutdownOrder = []string{
	"Shutdown signal received, shutting down",
	"Stopped the HTTP server",
	"Closed the websocket server",
	"Stopped the leader jobs",
	"Stopped the notification queue",
	"Stopped the cleanup cron",
	"Stopped the status repair",
	"Closed the event bus",
	"SQLite database closed gracefully",
	"Closed the database",
	"API stopped",
}

// shutdownOrderWithMetrics is shutdownOrder with METRICS_ENABLED, which adds the collector
var shutdownOrderWithMetrics = slices.Insert(slices.Clone(shutdownOrder), slices.Index(shutdownOrder, "Closed the event bus"), "Stopped the metrics collector")

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

// runWith runs the API on the listener with a logger the test observes, and returns what run
// returned. It fails the test if run takes longer than the shutdown deadline.
func runWith(t *testing.T, ctx context.Context, cfg *Config, listener net.Listener) (error, *observer.ObservedLogs) {
	t.Helper()
	logger, logs := observedLogger()
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg, logger, listener) }()
	select {
	case err := <-done:
		return err, logs
	case <-time.After(shutdownTimeouts.Total() + 5*time.Second):
		require.FailNow(t, "run didn't return within the shutdown deadline")
		return nil, logs
	}
}

func TestRun_StopsEverythingAndReturnsAnErrorWhenTheHTTPServerFails(t *testing.T) {
	cfg := loadConfig(t, nil)
	migrateDatabase(t, cfg)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	// ctx stays open: the failure alone ends run
	err, logs := runWith(t, context.Background(), cfg, listener)

	require.Error(t, err, "the API exits 1")
	assert.ErrorContains(t, err, "HTTP server failed")
	assert.ErrorContains(t, err, "use of closed network connection")
	failed := logs.FilterMessage("HTTP server failed, shutting down").All()
	require.Len(t, failed, 1)
	assert.Equal(t, zap.ErrorLevel, failed[0].Level)
	order := []string{
		"HTTP server failed, shutting down",
		"Stopped the HTTP server",
		"Closed the websocket server",
		"Stopped the notification queue",
		"Stopped the cleanup cron",
		"Stopped the status repair",
		"Closed the event bus",
		"SQLite database closed gracefully",
		"Closed the database",
	}
	assert.Equal(t, order, logged(logs, order), "everything that started stopped, in order")
	assert.Zero(t, logs.FilterMessage("API stopped").Len())
}

func TestRun_StopsWhatItStartedAndReturnsAnErrorWhenStartingFails(t *testing.T) {
	// The database isn't migrated, so the settings can't be initialized once the cleanup cron
	// has started
	cfg := loadConfig(t, nil)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	err, logs := runWith(t, context.Background(), cfg, listener)

	require.Error(t, err, "the API exits 1")
	assert.ErrorContains(t, err, "failed to initialize JWT settings")
	assert.Zero(t, logs.FilterMessage("Serving the API").Len(), "it never served")
	order := []string{
		"Stopped the HTTP server",
		"Closed the websocket server",
		"Stopped the cleanup cron",
		"Closed the event bus",
		"SQLite database closed gracefully",
		"Closed the database",
	}
	assert.Equal(t, order, logged(logs, order), "what had started stopped, in order")
	assert.Zero(t, logs.FilterMessage("API stopped").Len())

	_, err = net.Dial("tcp", listener.Addr().String())
	assert.Error(t, err, "the listener is closed")
}

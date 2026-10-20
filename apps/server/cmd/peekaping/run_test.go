package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/shared"

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
		require.Len(t, stored, 1)
		assert.Equal(t, pushed.hb.ID, stored[0].ID, "the heartbeat pushed is the one stored")
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

	// The long poll would hold up the HTTP server's shutdown until its next ping
	socket.close()
	require.NoError(t, in.stop(t))

	assert.Zero(t, redisConnections(), "peekaping never connects to Redis")
	ignored := logs.FilterMessage(ignoringRedis).All()
	require.Len(t, ignored, 1, "it says once that it ignores the Redis settings")
	assert.Equal(t, []any{"REDIS_HOST", "REDIS_PORT"}, ignored[0].ContextMap()["settings"])
}

func TestRun_ShutsDownInOrderOnceTheRequestsInFlightAreAnswered(t *testing.T) {
	cfg := loadConfig(t, nil)
	logger, logs := observedLogger()
	in := startPeekaping(t, cfg, logger, logs)
	in.registerAdmin(t)

	// A login whose body is still on its way when the shutdown starts
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, adminEmail, adminPassword)
	conn, err := net.Dial("tcp", strings.TrimPrefix(in.url, "http://"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "POST /api/v1/auth/login HTTP/1.1\r\nHost: peekaping\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		len(login), login[:10])
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond) // the server reads the headers and waits for the rest

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

	_, err = io.WriteString(conn, login[10:])
	require.NoError(t, err)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the login in flight is answered")

	select {
	case err := <-stopped:
		require.NoError(t, err, "run returns nil, so peekaping exits 0")
	case <-time.After(shutdownTimeout):
		require.FailNow(t, "run didn't return within the shutdown deadline")
	}
	t.Logf("shut down in %s", time.Since(startedAt).Round(time.Millisecond))

	order := []string{
		"Shutdown signal received, shutting down",
		"Stopped the HTTP server",
		"Closed the websocket server",
		"Stale-monitor watchdog stopped",
		"Scheduler stopped",
		"Stopped the scheduler, the watchdog and the other background jobs",
		"Closed the event bus",
		"SQLite database closed gracefully",
		"Closed the database",
		"Peekaping stopped",
	}
	assert.Equal(t, order, logged(logs, order), "the shutdown's steps, in the order logged")
	assert.Empty(t, logs.FilterMessage(ignoringRedis).All(), "no REDIS_* variable is set")
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

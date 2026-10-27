package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restoreServerTimeouts puts serverTimeouts back when the test ends. Call it before startPeekaping:
// cleanups run last in, first out, so the restore comes after the server stops.
func restoreServerTimeouts(t *testing.T) {
	t.Helper()
	saved := serverTimeouts
	t.Cleanup(func() { serverTimeouts = saved })
}

func TestRun_DisconnectsAClientThatSendsItsHeadersSlowly(t *testing.T) {
	restoreServerTimeouts(t)
	serverTimeouts.ReadHeader = 300 * time.Millisecond
	logger, logs := observedLogger()
	in := startPeekaping(t, loadConfig(t, nil), logger, logs)

	conn, err := net.Dial("tcp", strings.TrimPrefix(in.url, "http://"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, "GET /api/v1/health HTTP/1.1\r\nHost: peekaping\r\nX-Slow: ")
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	answer, err := io.ReadAll(conn)
	require.NoError(t, err, "the server closes the connection before the client gives up")
	assert.Empty(t, answer)
}

func TestRun_KeepsLongPollsAndWebsocketsOpenPastTheReadTimeout(t *testing.T) {
	const read = 400 * time.Millisecond
	restoreServerTimeouts(t)
	serverTimeouts.Read = read
	logger, logs := observedLogger()
	in := startPeekaping(t, loadConfig(t, nil), logger, logs)
	in.registerAdmin(t)
	pushID := in.createMonitor(t, pushMonitor("Backup", "backup-push-token"))

	t.Run("the read timeout is set: a body that stops arriving fails to read", func(t *testing.T) {
		conn, _ := in.sendHalfALogin(t) // the body's rest never comes
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		require.NoError(t, err, "the server stops waiting for the body")
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("a long poll answers after the read timeout", func(t *testing.T) {
		socket := in.dialSocket(t)
		socket.join(t, "monitor:all")
		socket.holdsAPollOpenFor(t, 3*read) // the long poll waits through the read timeout, with nothing to answer
		pushUntil(t, in, "long-poll-", func() { socket.join(t, "monitor:all") }, func(msg string) bool {
			_, ok := socket.until(t, 200*time.Millisecond, func(e socketEvent) bool { return e.hb.MonitorID == pushID && e.hb.Msg == msg })
			return ok
		})
	})

	t.Run("a websocket delivers after the read timeout", func(t *testing.T) {
		conn := in.dialWebsocket(t)
		require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(`42["join_room","monitor:all"]`)))
		require.NoError(t, conn.SetReadDeadline(time.Time{}))
		packets := make(chan string, 64)
		go func() {
			defer close(packets)
			for {
				_, packet, err := conn.ReadMessage()
				if err != nil {
					return
				}
				packets <- string(packet)
			}
		}()
		opened := time.Now()
		for time.Since(opened) < 3*read { // the websocket stays open through the read timeout
			select {
			case _, open := <-packets:
				require.True(t, open, "the websocket closed before the push")
			case <-time.After(10 * time.Millisecond):
			}
		}
		join := func() {
			require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(`42["join_room","monitor:all"]`)))
		}
		pushUntil(t, in, "websocket-", join, func(msg string) bool {
			timeout := time.After(200 * time.Millisecond)
			for {
				select {
				case packet, open := <-packets:
					if !open {
						return false
					}
					if strings.HasPrefix(packet, "42") && strings.Contains(packet, msg) {
						return true
					}
				case <-timeout:
					return false
				}
			}
		})
	})
}

// pushUntil pushes to the push monitor until arrived reports that a push reached the socket, and
// sends join again before each push but the first. The socket's join is handled in its own
// goroutine, so a push can go before it. And the server tells the client it is connected before
// its connection handler has registered join_room, so a join sent at once can be dropped for good.
func pushUntil(t *testing.T, in *instance, prefix string, join func(), arrived func(msg string) bool) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for i := 0; ; i++ {
		if i > 0 {
			join()
		}
		msg := fmt.Sprintf("%s%d", prefix, i)
		status, body := in.request(t, http.MethodGet, "/api/v1/push/backup-push-token?msg="+url.QueryEscape(msg), nil)
		require.Equal(t, http.StatusOK, status, string(body))
		if arrived(msg) {
			return
		}
		require.True(t, time.Now().Before(deadline), "no push reached the socket within %s", wait)
	}
}

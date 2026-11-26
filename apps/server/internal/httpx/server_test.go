package httpx_test

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"peekaping/internal/httpx"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveWithTimeouts runs httpx.NewServer on a free local port until the test ends, and returns its address.
func serveWithTimeouts(t *testing.T, timeouts httpx.Timeouts, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := httpx.NewServer("", handler, timeouts)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

func TestNewServer_ServesWithTheDefaultTimeoutsAndNoWriteTimeout(t *testing.T) {
	timeouts := httpx.DefaultTimeouts()
	assert.Equal(t, 10*time.Second, timeouts.ReadHeader)
	assert.Equal(t, 30*time.Second, timeouts.Read)
	assert.Equal(t, 120*time.Second, timeouts.Idle)

	server := httpx.NewServer(":8034", http.NotFoundHandler(), timeouts)
	assert.Equal(t, 10*time.Second, server.ReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, server.ReadTimeout)
	assert.Equal(t, 120*time.Second, server.IdleTimeout)
	// A socket.io long poll waits up to 25 seconds for its answer, so no write timeout
	assert.Zero(t, server.WriteTimeout)
}

func TestNewServer_RefusesARequestWithAHeaderBlockOver64KiB(t *testing.T) {
	addr := serveWithTimeouts(t, httpx.DefaultTimeouts(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	// A new connection for each request: Go counts up to 4 KiB more than MaxHeaderBytes, and
	// up to 4 KiB again on a connection that already served a request
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for _, tc := range []struct {
		name string
		size int
		want int
	}{
		{"60 KiB", 60 << 10, http.StatusNoContent},
		{"65 KiB", 65 << 10, http.StatusRequestHeaderFieldsTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
			require.NoError(t, err)
			req.Header.Set("X-Padding", strings.Repeat("a", tc.size))
			resp, err := client.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, tc.want, resp.StatusCode)
		})
	}
}

func TestNewServer_ReadTimeoutCutsOffASlowBodyButNotAWebsocket(t *testing.T) {
	timeouts := httpx.DefaultTimeouts()
	timeouts.Read = 300 * time.Millisecond
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			kind, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if conn.WriteMessage(kind, message) != nil {
				return
			}
		}
	})
	mux.HandleFunc("/body", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	addr := serveWithTimeouts(t, timeouts, mux)

	// The control: the ReadTimeout is set, and a body that stops arriving fails to read
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = io.WriteString(conn, "POST /body HTTP/1.1\r\nHost: peekaping\r\nContent-Length: 10\r\n\r\nabc")
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	require.NoError(t, err, "the server stops waiting for the body")
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusRequestTimeout, resp.StatusCode)

	// A websocket upgrade ends the server's deadlines, so it echoes after the ReadTimeout passed
	socket, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/echo", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = socket.Close() })
	time.Sleep(2 * timeouts.Read)
	require.NoError(t, socket.WriteMessage(websocket.TextMessage, []byte("still here")))
	require.NoError(t, socket.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, echo, err := socket.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "still here", string(echo))
}

func TestNewServer_ClosesAClientThatSendsItsHeadersSlowly(t *testing.T) {
	timeouts := httpx.DefaultTimeouts()
	timeouts.ReadHeader = 200 * time.Millisecond
	addr := serveWithTimeouts(t, timeouts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	// Half a header, and nothing after it
	_, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: peekaping\r\nX-Slow: ")
	require.NoError(t, err)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	startedAt := time.Now()
	answer, err := io.ReadAll(conn)
	require.NoError(t, err, "the server closes the connection before the client gives up")
	assert.Empty(t, answer)
	assert.Less(t, time.Since(startedAt), 2*time.Second)
}

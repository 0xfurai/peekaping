package httpx_test

import (
	"io"
	"net"
	"net/http"
	"peekaping/internal/httpx"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve runs httpx.NewServer on a free local port until the test ends, and returns its address.
func serve(t *testing.T, timeouts httpx.Timeouts, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := httpx.NewServer("", handler, timeouts)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

func TestNewServer_ClosesAClientThatSendsItsHeadersSlowly(t *testing.T) {
	timeouts := httpx.DefaultTimeouts()
	timeouts.ReadHeader = 200 * time.Millisecond
	addr := serve(t, timeouts, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

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

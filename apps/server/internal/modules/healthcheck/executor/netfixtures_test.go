package executor

import (
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// refusedAddr returns host:port of a port nothing listens on: it listens to
// get a free port, then closes the listener.
func refusedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	require.NoError(t, l.Close())
	return addr
}

// silentAddr is a server that accepts connections and never says a word.
func silentAddr(t *testing.T) string {
	t.Helper()
	addr, _ := silentServer(t)
	return addr
}

// silentServer starts a server that accepts connections and never sends a
// byte. It returns host:port, and how many connections it accepted.
func silentServer(t *testing.T) (addr string, accepted func() int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return l.Addr().String(), func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(conns)
	}
}

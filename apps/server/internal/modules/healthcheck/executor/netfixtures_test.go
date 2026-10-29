package executor

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// refusedAddr returns host:port of a port that refuses connections: the first of the low ports that
// does. A port below the range the system hands out for ":0" can't be taken by another test between
// this check and the test's dial, as a port that was only free a moment ago can.
func refusedAddr(t *testing.T) string {
	t.Helper()
	for port := 1; port < 100; port++ {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			continue
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			return addr
		}
	}
	t.Fatal("no port below 100 refuses connections on 127.0.0.1")
	return ""
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

package executor

import (
	"context"
	"fmt"
	"net"
	"peekaping/internal/modules/shared"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func tcpMonitor(t *testing.T, addr string) *Monitor {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return &Monitor{
		ID:      "tcp",
		Type:    "tcp",
		Name:    addr,
		Timeout: 2,
		Config:  fmt.Sprintf(`{"host": %q, "port": %d}`, host, port),
	}
}

func TestTCPExecutor_Execute_OpenPort(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			listenAddr := "127.0.0.1:0"
			if host == "::1" || host == "[::1]" {
				if !hasLoopbackIPv6() {
					t.Skip("no IPv6 loopback address")
				}
				listenAddr = "[::1]:0"
			}
			l, err := net.Listen("tcp", listenAddr)
			require.NoError(t, err)
			defer l.Close()
			port := l.Addr().(*net.TCPAddr).Port
			monitor := tcpMonitor(t, l.Addr().String())
			monitor.Config = fmt.Sprintf(`{"host": %q, "port": %d}`, host, port)

			result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			assert.Equal(t, fmt.Sprintf("TCP port %d is open", port), result.Message)
		})
	}
}

func TestTCPExecutor_Execute_ClosedPort(t *testing.T) {
	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), tcpMonitor(t, closedAddr(t)), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "TCP connection failed")
	assert.Contains(t, result.Message, "connection refused")
}

func TestTCPExecutor_Execute_InvalidConfig(t *testing.T) {
	monitor := &Monitor{ID: "tcp", Type: "tcp", Name: "tcp", Timeout: 2, Config: `{invalid json}`}

	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), monitor, nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "failed to parse config")
}

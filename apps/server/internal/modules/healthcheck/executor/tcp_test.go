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
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), tcpMonitor(t, l.Addr().String()), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, fmt.Sprintf("TCP port %d is open", port), result.Message)
}

// requireIPv6Loopback skips the test on a machine without an IPv6 loopback address.
func requireIPv6Loopback(t *testing.T) {
	t.Helper()
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	_ = l.Close()
}

func TestTCPExecutor_Execute_IPv6Host(t *testing.T) {
	requireIPv6Loopback(t)
	l, err := net.Listen("tcp", "[::1]:0")
	require.NoError(t, err)
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	result := NewTCPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), tcpMonitor(t, l.Addr().String()), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, fmt.Sprintf("TCP port %d is open", port), result.Message)
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

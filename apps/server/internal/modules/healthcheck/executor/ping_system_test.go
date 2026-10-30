package executor

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"peekaping/internal/modules/shared"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// installFakePing puts a "ping" script with the given body first on the PATH, and returns the file
// its arguments are written to, one per line.
func installFakePing(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the ping command")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\" >> '" + argsFile + "'; done\n" + body + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ping"), []byte(script), 0o755))
	t.Setenv("PATH", dir)
	return argsFile
}

func TestTrySystemPing_ReadsTheOutput(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantSuccess bool
		wantErr     string
	}{
		{name: "reply from", body: "echo 'Reply from 192.0.2.1: bytes=32 time=1ms TTL=64'", wantSuccess: true},
		{name: "packets transmitted", body: "echo '1 packets transmitted, 1 packets received, 0.0% packet loss'", wantSuccess: true},
		{name: "packets transmitted and all lost", body: "echo '1 packets transmitted, 0 packets received, 100% packet loss'"},
		{name: "100% loss", body: "echo 'Packets: Sent = 1, Received = 0, Lost = 1 (100% loss)'"},
		{name: "request timed out", body: "echo 'Request timed out.'"},
		{name: "destination host unreachable", body: "echo 'Reply from 192.0.2.9: Destination host unreachable.'"},
		{name: "unrecognised output", body: "echo 'something else'", wantErr: "unable to determine ping result from output: something else\n"},
		{name: "no output", body: "true", wantErr: "unable to determine ping result from output: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakePing(t, tt.body)

			success, rtt, err := NewPingExecutor(zap.NewNop().Sugar()).trySystemPing(context.Background(), "192.0.2.1", 32, time.Second)

			assert.Equal(t, tt.wantSuccess, success)
			assert.Positive(t, rtt, "the round trip is the time the command took")
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func TestTrySystemPing_PingCommandMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	success, rtt, err := NewPingExecutor(zap.NewNop().Sugar()).trySystemPing(context.Background(), "192.0.2.1", 32, time.Second)

	assert.False(t, success)
	assert.Zero(t, rtt)
	require.Error(t, err)
	assert.ErrorIs(t, err, exec.ErrNotFound)
	assert.Equal(t, `ping command failed: exec: "ping": executable file not found in $PATH`, err.Error())
}

func TestTrySystemPing_PassesThePacketSizeAndTimeout(t *testing.T) {
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = "-c\n1\n-s\n56\n-W\n2500\n192.0.2.1\n"
	case "windows":
		t.Skip("uses a shell script as the ping command")
	default:
		want = "-c\n1\n-s\n56\n-W\n2\n192.0.2.1\n"
	}
	argsFile := installFakePing(t, "echo '64 bytes from 192.0.2.1'")

	_, _, err := NewPingExecutor(zap.NewNop().Sugar()).trySystemPing(context.Background(), "192.0.2.1", 56, 2500*time.Millisecond)

	require.NoError(t, err)
	args, err := os.ReadFile(argsFile)
	require.NoError(t, err)
	assert.Equal(t, want, string(args))
}

func TestSystemPingArgs(t *testing.T) {
	timeout := 2500 * time.Millisecond

	tests := []struct {
		goos string
		want []string
	}{
		{"windows", []string{"-n", "1", "-l", "56", "-w", "2500", "192.0.2.1"}},
		{"darwin", []string{"-c", "1", "-s", "56", "-W", "2500", "192.0.2.1"}},
		{"linux", []string{"-c", "1", "-s", "56", "-W", "2", "192.0.2.1"}},
		{"freebsd", []string{"-c", "1", "-s", "56", "-W", "2", "192.0.2.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			assert.Equal(t, tt.want, systemPingArgs(tt.goos, "192.0.2.1", 56, timeout))
		})
	}
}

func TestPingExecutor_Execute_UnresolvableHost(t *testing.T) {
	result := NewPingExecutor(zap.NewNop().Sugar()).Execute(context.Background(), pingMonitor("no-such-host.invalid", 2), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "Ping failed: failed to resolve host: ")
}

// failingPacketConn fails to write, or to read, as set.
type failingPacketConn struct {
	writeErr error
	readErr  error
}

func (c *failingPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	return len(b), nil
}

func (c *failingPacketConn) ReadFrom([]byte) (int, net.Addr, error) {
	return 0, nil, c.readErr
}

func TestExchangeEcho_Failures(t *testing.T) {
	dst := &net.IPAddr{IP: net.ParseIP("192.0.2.1")}
	req := &icmp.Echo{ID: 7, Seq: 42, Data: []byte("Peekaping")}

	t.Run("the packet can't be sent", func(t *testing.T) {
		_, err := exchangeEcho(&failingPacketConn{writeErr: errors.New("network is unreachable")}, dst, req, false, true)

		assert.EqualError(t, err, "failed to send ICMP packet: network is unreachable")
	})

	t.Run("the socket fails while waiting", func(t *testing.T) {
		readErr := errors.New("socket closed")

		_, err := exchangeEcho(&failingPacketConn{readErr: readErr}, dst, req, false, true)

		assert.Same(t, readErr, err)
	})

	t.Run("a reply that is not an echo", func(t *testing.T) {
		unreachable, err := (&icmp.Message{Type: ipv4.ICMPTypeDestinationUnreachable, Body: &icmp.DstUnreach{Data: []byte("x")}}).Marshal(nil)
		require.NoError(t, err)
		conn := &fakePacketConn{packets: []fakePacket{{unreachable, dst}}}

		_, err = exchangeEcho(conn, dst, req, false, true)

		assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
		assert.Empty(t, conn.packets)
	})
}

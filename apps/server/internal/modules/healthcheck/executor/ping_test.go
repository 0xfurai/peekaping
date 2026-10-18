package executor

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"peekaping/internal/modules/shared"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

type fakePacket struct {
	data []byte
	from net.Addr
}

// fakePacketConn replays packets in order, then times out like a socket deadline.
type fakePacketConn struct {
	packets []fakePacket
}

func (c *fakePacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	return len(b), nil
}

func (c *fakePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if len(c.packets) == 0 {
		return 0, nil, os.ErrDeadlineExceeded
	}
	p := c.packets[0]
	c.packets = c.packets[1:]
	return copy(b, p.data), p.from, nil
}

func echoPacket(t *testing.T, typ icmp.Type, id, seq int) []byte {
	t.Helper()
	b, err := (&icmp.Message{Type: typ, Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("Peekaping")}}).Marshal(nil)
	require.NoError(t, err)
	return b
}

func TestExchangeEcho_SkipsPacketsThatAreNotItsReply(t *testing.T) {
	dst := &net.IPAddr{IP: net.ParseIP("192.0.2.1")}
	otherHost := &net.IPAddr{IP: net.ParseIP("192.0.2.2")}
	req := &icmp.Echo{ID: 7, Seq: 42, Data: []byte("Peekaping")}

	foreign := []fakePacket{
		{echoPacket(t, ipv4.ICMPTypeEcho, 7, 42), dst},            // our own request, looped back
		{echoPacket(t, ipv4.ICMPTypeEchoReply, 7, 42), otherHost}, // same ID and seq from another host
		{echoPacket(t, ipv4.ICMPTypeEchoReply, 7, 43), dst},       // another request to the same host
		{echoPacket(t, ipv4.ICMPTypeEchoReply, 8, 42), dst},       // another process
		{[]byte{0x00}, dst}, // not an ICMP message
	}

	t.Run("times out when only other packets arrive", func(t *testing.T) {
		conn := &fakePacketConn{packets: foreign}
		_, err := exchangeEcho(conn, dst, req, false, true)
		assert.ErrorIs(t, err, os.ErrDeadlineExceeded)
	})

	t.Run("returns when its reply arrives", func(t *testing.T) {
		packets := append(append([]fakePacket{}, foreign...), fakePacket{echoPacket(t, ipv4.ICMPTypeEchoReply, 7, 42), dst})
		conn := &fakePacketConn{packets: packets}
		_, err := exchangeEcho(conn, dst, req, false, true)
		assert.NoError(t, err)
		assert.Empty(t, conn.packets)
	})
}

func TestExchangeEcho_IgnoresIDRewrittenByKernel(t *testing.T) {
	dst := &net.UDPAddr{IP: net.ParseIP("192.0.2.1")}
	req := &icmp.Echo{ID: 7, Seq: 42, Data: []byte("Peekaping")}
	conn := &fakePacketConn{packets: []fakePacket{
		{echoPacket(t, ipv4.ICMPTypeEchoReply, 31337, 41), dst},
		{echoPacket(t, ipv4.ICMPTypeEchoReply, 31337, 42), dst},
	}}

	_, err := exchangeEcho(conn, dst, req, false, false)
	assert.NoError(t, err)
	assert.Empty(t, conn.packets)
}

func TestExchangeEcho_IPv6(t *testing.T) {
	dst := &net.IPAddr{IP: net.ParseIP("2001:db8::1")}
	req := &icmp.Echo{ID: 7, Seq: 42, Data: []byte("Peekaping")}

	conn := &fakePacketConn{packets: []fakePacket{
		{echoPacket(t, ipv6.ICMPTypeEchoRequest, 7, 42), dst},
		{echoPacket(t, ipv6.ICMPTypeEchoReply, 7, 42), &net.IPAddr{IP: net.ParseIP("2001:db8::2")}},
		{echoPacket(t, ipv6.ICMPTypeEchoReply, 7, 42), dst},
	}}
	_, err := exchangeEcho(conn, dst, req, true, true)
	assert.NoError(t, err)
	assert.Empty(t, conn.packets)
}

func TestResolvePingTarget(t *testing.T) {
	tests := []struct {
		host     string
		wantIP   string
		wantZone string
	}{
		{host: "127.0.0.1", wantIP: "127.0.0.1"},
		{host: "::1", wantIP: "::1"},
		{host: "fe80::1%eth0", wantIP: "fe80::1", wantZone: "eth0"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			dst, err := resolvePingTarget(context.Background(), tt.host)
			require.NoError(t, err)
			assert.Equal(t, tt.wantIP, dst.IP.String())
			assert.Equal(t, tt.wantZone, dst.Zone)
		})
	}
}

func TestTrySystemPing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the ping command")
	}

	tests := []struct {
		name        string
		script      string
		wantSuccess bool
		wantErr     string
	}{
		{
			name:        "reply",
			script:      "echo '64 bytes from 192.0.2.1: icmp_seq=1 ttl=64 time=1.0 ms'",
			wantSuccess: true,
		},
		{
			name:   "no reply",
			script: "echo '1 packets transmitted, 0 received, 100% packet loss'; exit 1",
		},
		{
			name:    "cannot run",
			script:  "echo 'ping: permission denied (are you root?)' >&2; exit 1",
			wantErr: "ping command failed: exit status 1: ping: permission denied (are you root?)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "ping"), []byte("#!/bin/sh\n"+tt.script+"\n"), 0o755))
			t.Setenv("PATH", dir)

			success, _, err := NewPingExecutor(zap.NewNop().Sugar()).trySystemPing(context.Background(), "192.0.2.1", 32, time.Second)

			assert.Equal(t, tt.wantSuccess, success)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

// skipWithoutICMP skips live ping tests where no ICMP socket can be opened.
func skipWithoutICMP(t *testing.T, v6 bool) {
	t.Helper()
	conn, _, err := listenICMP(v6)
	if err != nil {
		t.Skipf("no ICMP socket available: %v", err)
	}
	conn.Close()
}

// skipWithoutRouteToTestNet skips tests that expect pings to 192.0.2.0/24
// (TEST-NET-1, RFC 5737) to leave the host and go unanswered.
func skipWithoutRouteToTestNet(t *testing.T) {
	t.Helper()
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		t.Skipf("no route to 192.0.2.1: %v", err)
	}
	conn.Close()
}

func pingMonitor(host string, timeout int) *Monitor {
	return &Monitor{
		ID:      "ping-" + host,
		Type:    "ping",
		Name:    host,
		Timeout: timeout,
		Config:  fmt.Sprintf(`{"host": %q}`, host),
	}
}

func TestPingExecutor_Execute_ReachableHost(t *testing.T) {
	skipWithoutICMP(t, false)
	executor := NewPingExecutor(zap.NewNop().Sugar())

	result := executor.Execute(context.Background(), pingMonitor("127.0.0.1", 2), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Contains(t, result.Message, "Ping successful")
}

func TestPingExecutor_Execute_ReachableIPv6Host(t *testing.T) {
	skipWithoutICMP(t, true)
	if !hasLoopbackIPv6() {
		t.Skip("no IPv6 loopback address")
	}
	executor := NewPingExecutor(zap.NewNop().Sugar())

	result := executor.Execute(context.Background(), pingMonitor("::1", 2), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
}

func TestPingExecutor_Execute_UnreachableHost(t *testing.T) {
	skipWithoutICMP(t, false)
	skipWithoutRouteToTestNet(t)
	executor := NewPingExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{name: "no context deadline", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
		// HandleMonitorTick gives the executor a context that expires with the check timeout.
		{name: "context deadline equals the timeout", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.ctx()
			defer cancel()

			start := time.Now()
			// 192.0.2.0/24 is TEST-NET-1 (RFC 5737), so nothing answers there.
			result := executor.Execute(ctx, pingMonitor("192.0.2.1", 1), nil)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Equal(t, "Ping failed: no reply from 192.0.2.1 within 1s", result.Message)
			assert.Less(t, time.Since(start), 1500*time.Millisecond, "should give up after one timeout")
		})
	}
}

func TestPingExecutor_Execute_ContextCancelled(t *testing.T) {
	skipWithoutICMP(t, false)
	skipWithoutRouteToTestNet(t)
	executor := NewPingExecutor(zap.NewNop().Sugar())
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	result := executor.Execute(ctx, pingMonitor("192.0.2.1", 20), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "Ping failed: no reply from 192.0.2.1: context canceled", result.Message)
	assert.Less(t, time.Since(start), time.Second)
}

func TestPingExecutor_Execute_ContextDeadlineBeforeTimeout(t *testing.T) {
	skipWithoutICMP(t, false)
	skipWithoutRouteToTestNet(t)
	executor := NewPingExecutor(zap.NewNop().Sugar())
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := executor.Execute(ctx, pingMonitor("192.0.2.1", 20), nil)

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "Ping failed: no reply from 192.0.2.1 within 300ms", result.Message)
	assert.Less(t, time.Since(start), time.Second)
}

func TestPingExecutor_Execute_ConcurrentPingsGetTheirOwnReplies(t *testing.T) {
	skipWithoutICMP(t, false)
	skipWithoutRouteToTestNet(t)
	executor := NewPingExecutor(zap.NewNop().Sugar())

	const monitors = 50
	hosts := make([]string, monitors)
	results := make([]*Result, monitors)
	var wg sync.WaitGroup
	for i := range monitors {
		hosts[i] = "127.0.0.1"
		if i%2 == 1 {
			hosts[i] = fmt.Sprintf("192.0.2.%d", i)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = executor.Execute(context.Background(), pingMonitor(hosts[i], 2), nil)
		}()
	}
	wg.Wait()

	for i, result := range results {
		want := shared.MonitorStatusUp
		if i%2 == 1 {
			want = shared.MonitorStatusDown
		}
		assert.Equal(t, want, result.Status, "%s: %s", hosts[i], result.Message)
	}
}

func hasLoopbackIPv6() bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.Equal(net.IPv6loopback) {
			return true
		}
	}
	return false
}

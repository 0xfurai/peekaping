package netguard_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"peekaping/internal/netguard"
)

// Each range has an address just inside and one just outside it, so that a wrong prefix length
// shows on either side.
func TestBlocked(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		// loopback
		{"127.0.0.1", true},
		{"127.255.255.255", true},
		{"126.255.255.255", false},
		{"128.0.0.1", false},
		{"::1", true},
		{"::2", false},
		// "this network" and unspecified
		{"0.0.0.0", true},
		{"0.255.255.255", true},
		{"1.0.0.0", false},
		{"::", true},
		// RFC 1918
		{"10.0.0.0", true},
		{"10.255.255.255", true},
		{"9.255.255.255", false},
		{"11.0.0.0", false},
		{"172.16.0.0", true},
		{"172.31.255.255", true},
		{"172.15.255.255", false},
		{"172.32.0.0", false},
		{"192.168.0.0", true},
		{"192.168.255.255", true},
		{"192.167.255.255", false},
		{"192.169.0.0", false},
		// carrier-grade NAT
		{"100.64.0.0", true},
		{"100.127.255.255", true},
		{"100.63.255.255", false},
		{"100.128.0.0", false},
		// link-local, with the cloud metadata address
		{"169.254.0.0", true},
		{"169.254.169.254", true},
		{"169.254.255.255", true},
		{"169.253.255.255", false},
		{"169.255.0.0", false},
		{"fe80::1", true},
		{"febf:ffff::1", true},
		{"fec0::1", false},
		{"fe7f::1", false},
		// IPv6 unique local
		{"fc00::1", true},
		{"fdff:ffff::1", true},
		{"fbff::1", false},
		{"fe00::1", false},
		// multicast and broadcast
		{"224.0.0.1", true},
		{"239.255.255.255", true},
		{"223.255.255.255", false},
		{"240.0.0.1", false},
		{"255.255.255.255", true},
		{"255.255.255.254", false},
		{"ff02::1", true},
		{"fefe::1", false},
		// public
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
		// IPv4-mapped IPv6 counts as its IPv4 address
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.1.2.3", true},
		{"::ffff:169.254.169.254", true},
		{"::ffff:0.0.0.0", true},
		{"::ffff:8.8.8.8", false},
		// a zone doesn't hide a link-local address
		{"fe80::1%eth0", true},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			assert.Equal(t, tc.want, netguard.Blocked(netip.MustParseAddr(tc.addr)))
		})
	}
}

func TestBlocked_TheZeroAddressIsBlocked(t *testing.T) {
	assert.True(t, netguard.Blocked(netip.Addr{}))
}

func TestControl(t *testing.T) {
	g := netguard.New(true)
	tests := []struct {
		name, network, address string
		blocked                bool
	}{
		{"loopback tcp", "tcp4", "127.0.0.1:80", true},
		{"loopback tcp6", "tcp6", "[::1]:80", true},
		{"mapped", "tcp6", "[::ffff:10.0.0.1]:80", true},
		{"metadata", "tcp", "169.254.169.254:80", true},
		{"udp", "udp4", "10.0.0.1:53", true},
		{"icmp has no port", "ip4:icmp", "192.168.1.1", true},
		{"link-local with a zone", "tcp6", "[fe80::1%eth0]:80", true},
		{"public tcp", "tcp4", "8.8.8.8:443", false},
		{"public udp6", "udp6", "[2606:4700:4700::1111]:53", false},
		{"unix socket", "unix", "/var/run/docker.sock", true},
		{"unix datagram", "unixgram", "/tmp/x", true},
		{"unreadable address", "tcp", "not an address", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := g.Control(tc.network, tc.address, nil)
			if tc.blocked {
				assert.ErrorIs(t, err, netguard.ErrBlocked)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOff_EveryMethodLetsTheCallThrough(t *testing.T) {
	for name, g := range map[string]*netguard.Guard{"off": netguard.New(false), "nil": nil} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, g.Enabled())
			assert.NoError(t, g.Control("tcp4", "127.0.0.1:80", nil))
			assert.NoError(t, g.Control("unix", "/var/run/docker.sock", nil))
			assert.NoError(t, g.CheckLiteral("127.0.0.1"))
			addrs, err := g.ResolveAndCheck(context.Background(), "127.0.0.1")
			require.NoError(t, err)
			assert.Equal(t, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, addrs)
		})
	}
}

func TestOff_DialsALoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	conn, err := netguard.New(false).DialContext(context.Background(), "tcp", listener.Addr().String())

	require.NoError(t, err)
	conn.Close()
}

func TestDialContext_RefusesALoopbackListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	accepted := make(chan struct{}, 1)
	go func() {
		if conn, err := listener.Accept(); err == nil {
			accepted <- struct{}{}
			conn.Close()
		}
	}()

	_, err = netguard.New(true).DialContext(context.Background(), "tcp", listener.Addr().String())

	assert.ErrorIs(t, err, netguard.ErrBlocked)
	assert.ErrorContains(t, err, "target address is not allowed")
	assert.Empty(t, accepted, "the listener saw no connection")
}

func TestDialContext_ChecksTheAddressANameResolvesTo(t *testing.T) {
	_, err := netguard.New(true).DialContext(context.Background(), "tcp", "localhost:80")

	assert.ErrorIs(t, err, netguard.ErrBlocked)
}

func TestDialer_KeepsTheControlTheDialerHas(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	var seen []string
	dialer := &net.Dialer{Control: func(network, address string, _ syscall.RawConn) error {
		seen = append(seen, address)
		return nil
	}}

	// Only the loopback address is blocked here, so the guard lets a dial to the listener through
	// and the dialer's own Control still runs
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
	conn, err := guard.Dialer(dialer).DialContext(context.Background(), "tcp", listener.Addr().String())
	require.NoError(t, err)
	conn.Close()
	assert.Equal(t, []string{listener.Addr().String()}, seen)

	// A blocked address stops the dial before the dialer's Control is asked
	seen = nil
	_, err = guard.Dialer(dialer).DialContext(context.Background(), "tcp", "127.0.0.2:80")
	assert.ErrorIs(t, err, netguard.ErrBlocked)
	assert.Empty(t, seen)
}

func TestDialer_AGuardTurnedOnLaterRefuses(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	guard := netguard.New(false)
	dialer := guard.Dialer(&net.Dialer{})

	conn, err := dialer.DialContext(context.Background(), "tcp", listener.Addr().String())
	require.NoError(t, err)
	conn.Close()

	guard.Set(true)
	_, err = dialer.DialContext(context.Background(), "tcp", listener.Addr().String())
	assert.ErrorIs(t, err, netguard.ErrBlocked)
}

func TestResolveAndCheck(t *testing.T) {
	g := netguard.New(true)

	t.Run("a blocked literal", func(t *testing.T) {
		_, err := g.ResolveAndCheck(context.Background(), "10.1.2.3")
		assert.ErrorIs(t, err, netguard.ErrBlocked)
	})
	t.Run("a blocked literal in brackets", func(t *testing.T) {
		_, err := g.ResolveAndCheck(context.Background(), "[::1]")
		assert.ErrorIs(t, err, netguard.ErrBlocked)
	})
	t.Run("a name that resolves to a blocked address", func(t *testing.T) {
		_, err := g.ResolveAndCheck(context.Background(), "localhost")
		assert.ErrorIs(t, err, netguard.ErrBlocked)
	})
	t.Run("a public literal is returned", func(t *testing.T) {
		addrs, err := g.ResolveAndCheck(context.Background(), "8.8.8.8")
		require.NoError(t, err)
		assert.Equal(t, []netip.Addr{netip.MustParseAddr("8.8.8.8")}, addrs)
	})
	t.Run("a name that does not resolve", func(t *testing.T) {
		_, err := g.ResolveAndCheck(context.Background(), "nonexistent.invalid")
		require.Error(t, err)
		assert.NotErrorIs(t, err, netguard.ErrBlocked)
	})
}

func TestCheckLiteral(t *testing.T) {
	g := netguard.New(true)
	tests := []struct {
		host    string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"169.254.169.254", true},
		{"10.0.0.5", true},
		{"::1", true},
		{"[::1]", true},
		{"::ffff:192.168.1.1", true},
		{" 10.0.0.5 ", true},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false},
		{"example.com", false},
		// A name is checked when the check dials, since what it resolves to can change
		{"localhost", false},
		{"internal.corp.example", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			err := g.CheckLiteral(tc.host)
			if !tc.blocked {
				assert.NoError(t, err)
				return
			}
			var target *netguard.TargetError
			require.ErrorAs(t, err, &target)
			assert.Equal(t, "TARGET_NOT_ALLOWED", target.Code)
			assert.ErrorIs(t, err, netguard.ErrBlocked)
		})
	}
}

func TestSet_ConcurrentTurnOn(t *testing.T) {
	g := netguard.New(false)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			g.Set(true)
		}()
		go func() {
			defer wg.Done()
			_ = g.Control("tcp4", "127.0.0.1:80", nil)
			_ = g.Enabled()
		}()
	}
	wg.Wait()

	assert.True(t, g.Enabled())
	assert.True(t, errors.Is(g.Control("tcp4", "127.0.0.1:80", nil), netguard.ErrBlocked))
}

func TestNilGuard_SetDoesNothing(t *testing.T) {
	var g *netguard.Guard
	g.Set(true)
	assert.False(t, g.Enabled())
}

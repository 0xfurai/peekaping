// Package netguard keeps a monitor from reaching the internal network. When BLOCK_PRIVATE_TARGETS is
// on, a Guard refuses the addresses a check must not dial: loopback, private and link-local ranges
// (cloud metadata at 169.254.169.254 among them), multicast and the like. It checks the address a
// connection is really made to, after the name is resolved, so that a hostname that resolves to a
// blocked address, a redirect to one and a DNS answer that changes between lookups are all refused.
//
// The package imports the standard library and nothing from Peekaping, because the probe's binary
// links it too.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"syscall"
)

// ErrBlocked is what a Guard answers for an address a monitor must not reach. A check that meets it
// is Down with this message.
var ErrBlocked = errors.New("target address is not allowed")

// blockedPrefixes are the ranges a monitor can't reach while the guard is on. IPv4-mapped IPv6
// addresses are matched as their IPv4 address, so only the plain forms are listed.
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8",          // "this network", and the unspecified address 0.0.0.0
	"10.0.0.0/8",         // RFC 1918
	"100.64.0.0/10",      // carrier-grade NAT, which cloud providers use for their own networks
	"127.0.0.0/8",        // loopback
	"169.254.0.0/16",     // link-local, with the metadata service at 169.254.169.254
	"172.16.0.0/12",      // RFC 1918
	"192.168.0.0/16",     // RFC 1918
	"224.0.0.0/4",        // multicast
	"255.255.255.255/32", // broadcast
	"::/128",             // unspecified
	"::1/128",            // loopback
	"fc00::/7",           // unique local
	"fe80::/10",          // link-local
	"ff00::/8",           // multicast
)

func mustPrefixes(list ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, len(list))
	for i, entry := range list {
		prefixes[i] = netip.MustParsePrefix(entry)
	}
	return prefixes
}

// Blocked reports whether addr is in a range a monitor can't reach while the guard is on. An
// IPv4-mapped IPv6 address counts as its IPv4 address, and an address's zone is ignored. The zero
// Addr is blocked: it names no host a check could mean to reach.
func Blocked(addr netip.Addr) bool {
	return blockedBy(blockedPrefixes, addr)
}

func blockedBy(prefixes []netip.Prefix, addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	// Prefix.Contains is false for an address with a zone, so a link-local address written with
	// one would pass
	addr = addr.WithZone("").Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Guard decides which addresses a check may dial. It is switched on and off at run time, safe for
// concurrent use, and off it does nothing: every method lets the call through. A nil *Guard is off,
// so code that has none needs no check.
type Guard struct {
	on       atomic.Bool
	prefixes []netip.Prefix
}

// New creates a guard that is on when enabled
func New(enabled bool) *Guard {
	g := &Guard{prefixes: blockedPrefixes}
	g.on.Store(enabled)
	return g
}

// NewWithPrefixes creates an enabled guard that blocks only the given ranges. It is for tests,
// which serve on a loopback address and need a different one to be refused (a redirect from the
// first to the second); nothing else should loosen the table.
func NewWithPrefixes(prefixes ...netip.Prefix) *Guard {
	g := &Guard{prefixes: prefixes}
	g.on.Store(true)
	return g
}

// Set turns the guard on or off. The probe sets it from its own setting and the server's.
func (g *Guard) Set(enabled bool) {
	if g != nil {
		g.on.Store(enabled)
	}
}

// Enabled reports whether the guard refuses addresses
func (g *Guard) Enabled() bool { return g != nil && g.on.Load() }

func (g *Guard) blocked(addr netip.Addr) bool { return blockedBy(g.prefixes, addr) }

// Control is a net.Dialer.Control: the dialer calls it with the address it is about to connect to,
// after resolving the name, once for each address it tries. It refuses a blocked address, and any
// network that isn't TCP, UDP or IP (a Unix socket reaches what the machine shares with the
// server).
func (g *Guard) Control(network, address string, _ syscall.RawConn) error {
	if !g.Enabled() {
		return nil
	}
	if !strings.HasPrefix(network, "tcp") && !strings.HasPrefix(network, "udp") && !strings.HasPrefix(network, "ip") {
		return fmt.Errorf("%w: %s sockets", ErrBlocked, network)
	}
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil {
		// Not an address the guard can read, so it can't say the address is fine
		addr, addrErr := netip.ParseAddr(address)
		if addrErr != nil {
			return fmt.Errorf("%w: %q", ErrBlocked, address)
		}
		addrPort = netip.AddrPortFrom(addr, 0)
	}
	if g.blocked(addrPort.Addr()) {
		return fmt.Errorf("%w: %s", ErrBlocked, addrPort.Addr().WithZone(""))
	}
	return nil
}

// Dialer makes d dial through the guard and returns it. A Control the dialer already has still
// runs, after the guard's. The guard may be off now: it is asked at each dial, so it can be turned
// on later.
func (g *Guard) Dialer(d *net.Dialer) *net.Dialer {
	control, controlContext := d.Control, d.ControlContext
	d.Control = nil
	d.ControlContext = func(ctx context.Context, network, address string, c syscall.RawConn) error {
		if err := g.Control(network, address, c); err != nil {
			return err
		}
		if controlContext != nil {
			return controlContext(ctx, network, address, c)
		}
		if control != nil {
			return control(network, address, c)
		}
		return nil
	}
	return d
}

// DialContext connects to address through the guard, as net.Dialer's does
func (g *Guard) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return g.Dialer(&net.Dialer{}).DialContext(ctx, network, address)
}

// ResolveAndCheck resolves host and returns the addresses a check may use. It is for the paths that
// don't dial through a net.Dialer, such as an ICMP echo: the caller must use the returned address
// and not look host up again, or the answer could change between the check and the use. A literal
// address is returned as it is. If every address is blocked the error is ErrBlocked. The
// addresses are all of them when the guard is off.
func (g *Guard) ResolveAndCheck(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	var addrs []netip.Addr
	if addr, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{addr}
	} else {
		addrs, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
	}
	if !g.Enabled() {
		return addrs, nil
	}
	allowed := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if !g.blocked(addr) {
			allowed = append(allowed, addr)
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrBlocked, host)
	}
	return allowed, nil
}

// TargetError is a monitor or proxy setting that names a host the guard refuses, as the API answers
// it: 400 with this error in the body. Code tells a client this refusal from any other 400.
type TargetError struct {
	Code string `json:"code" example:"TARGET_NOT_ALLOWED"`
	Host string `json:"host" doc:"The address that is not allowed." example:"169.254.169.254"`
}

// CodeTargetNotAllowed is the Code of a TargetError
const CodeTargetNotAllowed = "TARGET_NOT_ALLOWED"

func (e *TargetError) Error() string { return ErrBlocked.Error() + ": " + e.Host }

func (e *TargetError) Unwrap() error { return ErrBlocked }

// CheckLiteral is the check at save time: it refuses host when it is an IP address, in brackets or
// not, that is blocked. A name passes, since what it resolves to can change and is checked when the
// check dials; this only keeps an address that can never work from being saved.
func (g *Guard) CheckLiteral(host string) error {
	if !g.Enabled() {
		return nil
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(host), "["), "]")
	addr, err := netip.ParseAddr(trimmed)
	if err != nil || !g.blocked(addr) {
		return nil
	}
	return &TargetError{Code: CodeTargetNotAllowed, Host: host}
}

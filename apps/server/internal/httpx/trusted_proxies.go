package httpx

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies is the set of addresses whose forwarding headers (X-Forwarded-For and
// X-Real-IP) the server believes. It is the TRUSTED_PROXIES setting, parsed once at startup.
//
// Any client can send those headers, so they say who the client is only when a proxy the
// operator runs set them. A request from outside the set is attributed to the address it
// connected from. The zero value and a nil *TrustedProxies trust nobody.
type TrustedProxies struct {
	prefixes []netip.Prefix
}

// ParseTrustedProxies parses a comma-separated list of IP addresses and CIDR ranges, such as
// "127.0.0.1,10.0.0.0/8,fc00::/7". An empty list trusts nobody. An entry that is neither an
// address nor a range is an error that names it.
func ParseTrustedProxies(list string) (*TrustedProxies, error) {
	proxies := &TrustedProxies{}
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := parseProxyEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXIES entry %q: %w", entry, err)
		}
		proxies.prefixes = append(proxies.prefixes, prefix)
	}
	return proxies, nil
}

var (
	errNotAddrOrRange = errors.New("want an IP address or a CIDR range such as 10.0.0.0/8")
	errShortMapped    = errors.New("an IPv4-mapped IPv6 range must be /96 or longer, because addresses are matched as IPv4")
)

func parseProxyEntry(entry string) (netip.Prefix, error) {
	if !strings.Contains(entry, "/") {
		addr, err := parseAddr(entry)
		if err != nil {
			return netip.Prefix{}, errNotAddrOrRange
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	prefix, err := netip.ParsePrefix(entry)
	if err != nil {
		return netip.Prefix{}, errNotAddrOrRange
	}
	// A range written as IPv4-mapped IPv6 (::ffff:10.0.0.0/104) is the IPv4 range. Addresses are
	// unmapped before they are matched, so a mapped range shorter than /96 would match nothing:
	// it is refused rather than accepted as a setting that does nothing.
	if addr := prefix.Addr(); addr.Is4In6() {
		if prefix.Bits() < 96 {
			return netip.Prefix{}, errShortMapped
		}
		return netip.PrefixFrom(addr.Unmap(), prefix.Bits()-96), nil
	}
	return prefix, nil
}

// parseAddr parses an IP address without a zone, with IPv4-mapped IPv6 addresses as IPv4.
func parseAddr(s string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, err
	}
	if addr.Zone() != "" {
		return netip.Addr{}, fmt.Errorf("address %q has a zone", s)
	}
	return addr.Unmap(), nil
}

func (p *TrustedProxies) trusts(addr netip.Addr) bool {
	if p == nil {
		return false
	}
	for _, prefix := range p.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the address of the client that sent r, or "" when r has no usable
// connection address.
//
// A request from an address outside the trusted proxies comes from the address it connected
// from, and its forwarding headers are ignored. For a request from a trusted proxy, the client
// is the rightmost X-Forwarded-For entry that isn't itself a trusted proxy: each proxy appends
// the address it received the request from, so everything left of that entry was written by
// the client or by someone further out than any proxy we trust. When every entry is a trusted
// proxy, the client is the leftmost one. An X-Forwarded-For the walk can't resolve (an entry
// that isn't a plain address, such as "1.2.3.4:5", "[::1]" or "unknown") makes the client the
// proxy itself: X-Real-IP is no fallback then, since a proxy that doesn't set it passes on the
// client's own. Only a request with no X-Forwarded-For header at all uses X-Real-IP, and
// without that too, the client is the proxy.
func (p *TrustedProxies) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return ""
	}
	peer, err := parseAddr(host)
	if err != nil || !p.trusts(peer) {
		return host
	}
	if forwardedFor := r.Header.Values("X-Forwarded-For"); len(forwardedFor) > 0 {
		if client, ok := p.forwardedClient(strings.Join(forwardedFor, ",")); ok {
			return client.String()
		}
		return peer.String()
	}
	if client, err := parseAddr(r.Header.Get("X-Real-IP")); err == nil {
		return client.String()
	}
	return peer.String()
}

// forwardedClient walks an X-Forwarded-For value from its right end, skipping trusted
// proxies. An entry that isn't an address ends the walk with no client, since a proxy we trust
// would not have written it.
func (p *TrustedProxies) forwardedClient(forwardedFor string) (client netip.Addr, ok bool) {
	rest := forwardedFor
	for {
		entry := rest
		next := strings.LastIndexByte(rest, ',')
		if next >= 0 {
			entry = rest[next+1:]
		}
		addr, err := parseAddr(entry)
		if err != nil {
			return netip.Addr{}, false
		}
		client = addr
		if !p.trusts(addr) || next < 0 {
			return client, true
		}
		rest = rest[:next]
	}
}

// Package hostport joins a host and a port into an address, for a host that a user typed and may
// have saved with brackets around an IPv6 address.
package hostport

import (
	"net"
	"strconv"
	"strings"
)

// Bare returns host without the brackets of an IPv6 address: "[::1]" becomes "::1". Only one
// matching pair around something is removed, so a host without brackets, with an unmatched
// bracket or with nothing between them is returned as it is. An empty host would dial this machine.
func Bare(host string) string {
	if len(host) >= 3 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

// Join returns the host:port address to dial, with brackets around an IPv6 host, whether host was
// saved with them or not. net.JoinHostPort alone would bracket a bracketed host a second time.
func Join(host string, port int) string {
	return net.JoinHostPort(Bare(host), strconv.Itoa(port))
}

package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"peekaping/internal/netguard"
)

// httpClientTimeout bounds each request a provider makes, so an endpoint that never
// answers can't hold up a notification
const httpClientTimeout = 10 * time.Second

// NewHTTPClient returns the client providers send with. One client is shared by all
// providers, so they reuse connections.
func NewHTTPClient() *http.Client {
	return NewGuardedHTTPClient(nil)
}

// NewGuardedHTTPClient is NewHTTPClient for a client that dials through the guard while it is on
// (BLOCK_PRIVATE_TARGETS, PP-158). The guard checks the address each connection is made to, after the
// name is resolved, so a URL that is a blocked address, a name that resolves to one and a redirect to
// one are all refused. A request that goes through a proxy from the server's environment
// (HTTPS_PROXY, HTTP_PROXY) dials the proxy, which is the install's own, unchecked, and its target is
// checked before it is sent (see guardedTransport). A guard that is off, or nil, leaves the client as
// NewHTTPClient makes it, with the default transport.
func NewGuardedHTTPClient(guard *netguard.Guard) *http.Client {
	client := &http.Client{
		Timeout:       httpClientTimeout,
		CheckRedirect: rejectHTTPSDowngrade,
	}
	if guard.Enabled() {
		var base *http.Transport
		if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
			base = defaultTransport.Clone()
		} else {
			// Something replaced the default transport, as a test does
			base = &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
		}
		// The direct transport's dialer is the default transport's, with the guard's check added
		direct := base.Clone()
		direct.Proxy = nil
		direct.DialContext = guard.Dialer(&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		client.Transport = &guardedTransport{guard: guard, proxy: base.Proxy, direct: direct, proxied: base}
	}
	return client
}

// guardedTransport sends a request straight to its host through the guard's dialer, or through
// the proxy that the server's environment names for it. The proxy is dialed as the default
// transport does: the guard would check the proxy's address, not the target's. So the target is
// checked before the request goes to the proxy instead, each address its name resolves to: the
// proxy resolves the name itself, and may use any of them. The client sends each redirect through
// RoundTrip again, so a redirect to a blocked address is refused as well.
type guardedTransport struct {
	guard *netguard.Guard
	// proxy picks the proxy for a request, nil for none: http.ProxyFromEnvironment
	proxy   func(*http.Request) (*url.URL, error)
	direct  *http.Transport
	proxied *http.Transport
}

func (t *guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var proxyURL *url.URL
	if t.proxy != nil {
		var err error
		if proxyURL, err = t.proxy(req); err != nil {
			closeRequestBody(req)
			return nil, err
		}
	}
	if proxyURL == nil {
		return t.direct.RoundTrip(req)
	}
	if err := t.checkTarget(req.Context(), req.URL.Hostname()); err != nil {
		closeRequestBody(req)
		return nil, err
	}
	return t.proxied.RoundTrip(req)
}

// checkTarget refuses a host that is a blocked address, or a name any of whose addresses is
// blocked
func (t *guardedTransport) checkTarget(ctx context.Context, host string) error {
	if err := t.guard.CheckLiteral(host); err != nil {
		return fmt.Errorf("%w: %s", netguard.ErrBlocked, host)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		if t.guard.CheckLiteral(addr.Unmap().String()) != nil {
			return fmt.Errorf("%w: %s", netguard.ErrBlocked, addr.Unmap())
		}
	}
	return nil
}

// closeRequestBody closes the body of a request that a RoundTrip doesn't send, as RoundTrip must
func closeRequestBody(req *http.Request) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
}

// rejectHTTPSDowngrade stops redirects from HTTPS to HTTP. net/http keeps the
// Authorization header on same-host redirects, so a token would go out in cleartext.
// It fails the request rather than returning the redirect, which providers that only
// check for 4xx and 5xx statuses would count as sent.
func rejectHTTPSDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refused a %s redirect from HTTPS to HTTP", req.Response.Status)
	}
	return nil
}

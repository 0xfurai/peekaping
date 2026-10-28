package providers

import (
	"errors"
	"fmt"
	"net"
	"net/http"
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
// one are all refused. A guard that is off, or nil, leaves the client as NewHTTPClient makes it, with
// the default transport.
func NewGuardedHTTPClient(guard *netguard.Guard) *http.Client {
	client := &http.Client{
		Timeout:       httpClientTimeout,
		CheckRedirect: rejectHTTPSDowngrade,
	}
	if guard.Enabled() {
		var transport *http.Transport
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			transport = base.Clone()
		} else {
			// Something replaced the default transport, as a test does
			transport = &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
		}
		// The dialer is the default transport's, with the guard's check added
		transport.DialContext = guard.Dialer(&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		client.Transport = transport
	}
	return client
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

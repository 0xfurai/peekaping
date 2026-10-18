package providers

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// httpClientTimeout bounds each request a provider makes, so an endpoint that never
// answers can't hold up a notification
const httpClientTimeout = 10 * time.Second

// NewHTTPClient returns the client providers send with. One client is shared by all
// providers, so they reuse connections.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       httpClientTimeout,
		CheckRedirect: rejectHTTPSDowngrade,
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

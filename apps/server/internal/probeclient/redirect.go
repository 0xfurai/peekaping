package probeclient

import (
	"errors"
	"fmt"
	"net/http"
)

// maxRedirects is how many redirects a request follows before it fails, as net/http's default
// client does
const maxRedirects = 10

// errRedirectDowngrade is why the probe won't follow a redirect from https to http
var errRedirectDowngrade = errors.New("refusing a redirect from https to http: the probe token would be sent in plain text")

// guardRedirects returns a client that makes its requests as client does, but refuses a redirect
// from https to http. The probe validates only the URL it is configured with, and Go keeps the
// Authorization header on a redirect to the same host, so a server on https that redirected the
// probe to http would be given the token in plain text.
//
// A nil client means http.DefaultClient. The client given is copied, not changed: its own rule
// for redirects (net/http's default of ten when it has none) is asked after this one.
func guardRedirects(client *http.Client) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	next := client.CheckRedirect
	guarded := *client
	guarded.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme == "http" {
			return errRedirectDowngrade
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		return nil
	}
	return &guarded
}

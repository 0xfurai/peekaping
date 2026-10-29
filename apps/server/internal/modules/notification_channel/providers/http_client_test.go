package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/netguard"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPClient_RefusesRedirectFromHTTPSToHTTP(t *testing.T) {
	var plainHit atomic.Bool
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainHit.Store(true)
	}))
	defer plain.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/hook", http.StatusTemporaryRedirect)
	}))
	defer secure.Close()
	client := NewHTTPClient()
	client.Transport = secure.Client().Transport

	resp, err := client.Post(secure.URL+"/hook", "application/json", nil)

	// An error, not the 3xx response: providers that only check for 4xx and 5xx would
	// report the redirect as sent
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "307 Temporary Redirect")
	assert.False(t, plainHit.Load(), "credentials must not follow a redirect to plain HTTP")
}

// With the guard on, a proxy from the server's environment (HTTP_PROXY, HTTPS_PROXY) is the
// install's own, so it is dialed even on a private address. What it would dial is checked before
// the request goes to it: a target at a blocked address, or a redirect to one, never reaches the
// proxy. Go reads the proxy variables once per process, so the test runs itself again with a proxy
// set, the way the standard library's own tests do.
func TestNewGuardedHTTPClient_SendsThroughAnEnvironmentProxyAndChecksTheTargetFirst(t *testing.T) {
	if os.Getenv("PEEKAPING_TEST_CHANNEL_PROXY") != "" {
		client := NewGuardedHTTPClient(netguard.New(true))

		resp, err := client.Get("http://203.0.113.10/hook")
		require.NoError(t, err, "the proxy is on loopback, which the guard blocks for a direct request")
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "via proxy", string(body))

		for _, url := range []string{"http://169.254.169.254/latest/meta-data", "http://203.0.113.10/redirect"} {
			resp, err := client.Get(url)
			if resp != nil {
				resp.Body.Close()
			}
			require.Error(t, err, url)
			assert.Contains(t, err.Error(), "target address is not allowed", url)
		}
		return
	}

	var mu sync.Mutex
	var seen []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.String())
		mu.Unlock()
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://10.0.0.1/inside", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "via proxy")
	}))
	defer proxy.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNewGuardedHTTPClient_SendsThroughAnEnvironmentProxyAndChecksTheTargetFirst$", "-test.v")
	cmd.Env = append(os.Environ(), "PEEKAPING_TEST_CHANNEL_PROXY=1",
		"HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL, "NO_PROXY=", "no_proxy=")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	// A renamed test would match nothing and exit 0.
	require.Contains(t, string(out), "--- PASS: TestNewGuardedHTTPClient_SendsThroughAnEnvironmentProxyAndChecksTheTargetFirst", string(out))
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"http://203.0.113.10/hook", "http://203.0.113.10/redirect"}, seen, "the blocked targets never reached the proxy")
}

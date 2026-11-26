package probeclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"peekaping/internal/probeclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTLSServer starts a stand-in for a server on https, whose Client trusts its certificate
func newTLSServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server
}

// The probe validates the URL it is configured with, not the ones a server redirects it to. Go
// keeps the Authorization header on a redirect to the same host, so a server on https that sends
// the probe to http on that host would be given the token in plain text.
func TestSession_NeverSendsTheTokenToPlainHTTP(t *testing.T) {
	var mu sync.Mutex
	var authorizations []string
	plain := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		reply(w, http.StatusOK, welcomeFrom(time.Now()))
	})
	secure := newTLSServer(t, http.RedirectHandler(plain.URL+"/probe/v1/hello", http.StatusTemporaryRedirect))
	logger, logs := observedLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pause := &sleeper{after: func(int, time.Duration) { cancel() }}
	session := newSession(secure.URL, func(c *probeclient.SessionConfig) {
		c.HTTPClient, c.Logger, c.Sleep = secure.Client(), logger, pause.Sleep
	})

	err := session.Connect(ctx)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, authorizations, "the server on plain http got no request, and so no token")
	assert.ErrorIs(t, err, context.Canceled, "the redirect failed the hello like a lost server, and the probe went on trying")
	warnings := logged(logs, "warn")
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "redirect from https to http", "and it logged why")
}

// A client the caller gives keeps its own rule for redirects, which the session asks after its own,
// and is not changed by the session.
func TestSession_KeepsTheRedirectRuleOfTheClientItIsGiven(t *testing.T) {
	secure := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("moved") == "" {
			http.Redirect(w, r, r.URL.Path+"?moved=1", http.StatusTemporaryRedirect)
			return
		}
		reply(w, http.StatusOK, welcomeFrom(time.Now()))
	}))
	var asked atomic.Int32
	client := secure.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		asked.Add(1)
		return nil
	}
	session := newSession(secure.URL, func(c *probeclient.SessionConfig) { c.HTTPClient = client })

	require.NoError(t, session.Connect(context.Background()), "a redirect from https to https is followed")

	assert.EqualValues(t, 1, asked.Load(), "the caller's rule was asked about it")
	from := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	to := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	assert.NoError(t, client.CheckRedirect(to, []*http.Request{from}), "the caller's client still allows what the session's refuses")
}

// Without a client of its own the session uses net/http's default one, but for the rule on redirects
func TestGuardRedirects_ForNoClientIsTheDefaultClientThatRefusesAnHTTPSDowngrade(t *testing.T) {
	t.Run("is the default client but for its redirect rule", func(t *testing.T) {
		got := *probeclient.GuardRedirects(nil)

		require.NotNil(t, got.CheckRedirect)
		got.CheckRedirect = nil
		assert.Equal(t, *http.DefaultClient, got)
	})

	t.Run("refuses a redirect from https to http", func(t *testing.T) {
		var requests atomic.Int32
		plain := newServer(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })
		secure := newTLSServer(t, http.RedirectHandler(plain.URL, http.StatusFound))
		client := probeclient.GuardRedirects(nil)
		client.Transport = secure.Client().Transport // to trust the test server's certificate

		resp, err := client.Get(secure.URL)

		if err == nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err)
		assert.ErrorContains(t, err, "redirect from https to http")
		assert.Zero(t, requests.Load(), "the server on plain http was not asked")
	})

	t.Run("follows ten redirects, as the default client does", func(t *testing.T) {
		var requests atomic.Int32
		var server *httptest.Server
		server = newServer(t, func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			http.Redirect(w, r, server.URL, http.StatusFound)
		})

		resp, err := probeclient.GuardRedirects(nil).Get(server.URL)

		if err == nil {
			_ = resp.Body.Close()
		}
		require.Error(t, err)
		assert.ErrorContains(t, err, "stopped after 10 redirects")
		assert.EqualValues(t, 10, requests.Load())
	})
}

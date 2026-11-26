package executor

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"peekaping/internal/modules/shared"
	"peekaping/internal/netguard"
)

// blockedMessage is what a check says when the guard refuses the address it would dial
const blockedMessage = "target address is not allowed"

// guardedExecutor is the executor of a monitor type as a registry with the guard builds it, which
// is how the server and the probe get theirs
func guardedExecutor(t *testing.T, monitorType string, guard *netguard.Guard) Executor {
	t.Helper()
	e, ok := NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(guard)).GetExecutor(monitorType)
	require.True(t, ok, monitorType)
	return e
}

// countingServer is an HTTP server that counts the requests it gets
func countingServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if handler != nil {
			handler(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func requireBlocked(t *testing.T, result *Result) {
	t.Helper()
	assert.Equal(t, shared.MonitorStatusDown, result.Status, result.Message)
	assert.Contains(t, result.Message, blockedMessage)
}

func TestHTTPExecutor_BlocksPrivateTargets(t *testing.T) {
	server, requests := countingServer(t, nil)
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	tests := []struct {
		name string
		cfg  HTTPConfig
	}{
		{"a literal loopback address", HTTPConfig{Url: server.URL, AuthMethod: "none"}},
		{"a name that resolves to loopback", HTTPConfig{Url: "http://localhost:" + port, AuthMethod: "none"}},
		{"the cloud metadata address", HTTPConfig{Url: "http://169.254.169.254/latest/meta-data", AuthMethod: "none"}},
		{"basic auth", HTTPConfig{Url: server.URL, AuthMethod: "basic", BasicAuthUser: "u", BasicAuthPass: "p"}},
		{"ntlm auth", HTTPConfig{Url: server.URL, AuthMethod: "ntlm", BasicAuthUser: "u", BasicAuthPass: "p", AuthDomain: "d", AuthWorkstation: "w"}},
		{"ignoring TLS errors", HTTPConfig{Url: server.URL, AuthMethod: "none", IgnoreTlsErrors: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := guardedExecutor(t, "http", netguard.New(true))

			result := e.Execute(context.Background(), httpMonitor(t, "http", tc.cfg), nil)

			requireBlocked(t, result)
		})
	}
	assert.Zero(t, requests.Load(), "the server saw no request")
}

func TestHTTPExecutor_BlocksPrivateTargets_MTLS(t *testing.T) {
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))
	var requests atomic.Int32
	server := startTLSServer(t, pki, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	e := guardedExecutor(t, "http", netguard.New(true))

	result := e.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
		Url: server.URL, AuthMethod: "mtls", TlsCert: pki.clientCertPEM, TlsKey: pki.clientKeyPEM, TlsCa: pki.caPEM,
	}), nil)

	requireBlocked(t, result)
	assert.Zero(t, requests.Load())
}

func TestHTTPExecutor_BlocksPrivateTargets_Off(t *testing.T) {
	server, requests := countingServer(t, nil)
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))
	tlsServer := startTLSServer(t, pki, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for name, guard := range map[string]*netguard.Guard{"off": netguard.New(false), "none": nil} {
		t.Run(name, func(t *testing.T) {
			e := guardedExecutor(t, "http", guard)

			plain := e.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{Url: server.URL, AuthMethod: "none"}), nil)
			assert.Equal(t, shared.MonitorStatusUp, plain.Status, plain.Message)

			mtls := e.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
				Url: tlsServer.URL, AuthMethod: "mtls", TlsCert: pki.clientCertPEM, TlsKey: pki.clientKeyPEM, TlsCa: pki.caPEM,
			}), nil)
			assert.Equal(t, shared.MonitorStatusUp, mtls.Status, mtls.Message)
		})
	}
	assert.Equal(t, int32(2), requests.Load())
}

// A redirect is a new dial on the same transport, so the guard refuses the one that leads to a
// blocked address even when the first address is allowed
func TestHTTPExecutor_BlocksARedirectToAPrivateTarget(t *testing.T) {
	blockedHost := netip.MustParseAddr("127.0.0.2")
	// Only 127.0.0.2 is blocked, so that the test can serve on 127.0.0.1 and redirect to it
	guard := netguard.NewWithPrefixes(netip.PrefixFrom(blockedHost, 32))

	// Where the redirect leads is listened on when the machine has the address; on one that
	// doesn't (macOS only has 127.0.0.1 up), the dial would be refused either way
	var blockedRequests atomic.Int32
	target := "127.0.0.2:" + strconv.Itoa(closedPort(t))
	if listener, err := net.Listen("tcp", "127.0.0.2:0"); err == nil {
		blockedServer := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			blockedRequests.Add(1)
		})}}
		blockedServer.Start()
		t.Cleanup(blockedServer.Close)
		target = listener.Addr().String()
	}
	allowed, allowedRequests := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://"+target+"/", http.StatusFound)
	})
	e := guardedExecutor(t, "http", guard)

	result := e.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{Url: allowed.URL, AuthMethod: "none", MaxRedirects: intPtr(5)}), nil)

	requireBlocked(t, result)
	assert.Equal(t, int32(1), allowedRequests.Load(), "the first address was reached")
	assert.Zero(t, blockedRequests.Load(), "the address the redirect leads to saw no request")
}

func TestHTTPExecutor_FollowsARedirectToAnAllowedTarget(t *testing.T) {
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.2/32"))
	final, finalRequests := countingServer(t, nil)
	first, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	})
	e := guardedExecutor(t, "http", guard)

	result := e.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{Url: first.URL, AuthMethod: "none", MaxRedirects: intPtr(5)}), nil)

	assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	assert.Equal(t, int32(1), finalRequests.Load())
}

func TestHTTPExecutor_BlocksAProxyAtAPrivateAddress(t *testing.T) {
	// The check dials the proxy, and the proxy dials the target, so a proxy at a blocked address is
	// what the guard sees. The URL is a name that never resolves: the check never gets that far.
	cfg := HTTPConfig{Url: "http://target.invalid/", AuthMethod: "none"}

	t.Run("an HTTP proxy", func(t *testing.T) {
		proxyServer, requests := countingServer(t, nil)
		host, port, err := net.SplitHostPort(proxyServer.Listener.Addr().String())
		require.NoError(t, err)
		portNumber, _ := strconv.Atoi(port)
		proxy := &Proxy{Protocol: "http", Host: host, Port: portNumber}

		blocked := guardedExecutor(t, "http", netguard.New(true)).Execute(context.Background(), httpMonitor(t, "http", cfg), proxy)
		requireBlocked(t, blocked)
		assert.Zero(t, requests.Load(), "the proxy saw no request")

		// With the option off the proxy answers for the target
		off := guardedExecutor(t, "http", netguard.New(false)).Execute(context.Background(), httpMonitor(t, "http", cfg), proxy)
		assert.Equal(t, shared.MonitorStatusUp, off.Status, off.Message)
		assert.Equal(t, int32(1), requests.Load())
	})

	t.Run("a SOCKS proxy", func(t *testing.T) {
		address, accepted := silentServer(t)
		host, port, err := net.SplitHostPort(address)
		require.NoError(t, err)
		portNumber, _ := strconv.Atoi(port)
		proxy := &Proxy{Protocol: "socks5", Host: host, Port: portNumber}
		monitor := httpMonitor(t, "http", cfg)
		monitor.Timeout = 1

		blocked := guardedExecutor(t, "http", netguard.New(true)).Execute(context.Background(), monitor, proxy)
		requireBlocked(t, blocked)
		assert.Zero(t, accepted(), "the proxy saw no connection")

		// With the option off the check connects to the proxy, which says nothing back
		off := guardedExecutor(t, "http", netguard.New(false)).Execute(context.Background(), monitor, proxy)
		assert.Equal(t, shared.MonitorStatusDown, off.Status)
		assert.NotContains(t, off.Message, blockedMessage)
		assert.Equal(t, 1, accepted())
	})
}

func TestHTTPExecutor_BlocksAnOAuthTokenURLAtAPrivateAddress(t *testing.T) {
	tokenServer, tokenRequests := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token": "t", "token_type": "Bearer"}`))
	})
	api, apiRequests := countingServer(t, nil)
	cfg := HTTPConfig{
		Url: api.URL, AuthMethod: "oauth2-cc", OauthAuthMethod: "client_secret_post",
		OauthTokenUrl: tokenServer.URL + "/token", OauthClientId: "id", OauthClientSecret: "secret",
	}

	// The token request comes before the one to the API, and the token server is on loopback
	guard := netguard.NewWithPrefixes(netip.MustParsePrefix("127.0.0.1/32"))

	blocked := guardedExecutor(t, "http", guard).Execute(context.Background(), httpMonitor(t, "http", cfg), nil)

	requireBlocked(t, blocked)
	assert.Contains(t, blocked.Message, "oauth2 token")
	assert.Zero(t, tokenRequests.Load(), "the token server saw no request")
	assert.Zero(t, apiRequests.Load())

	off := guardedExecutor(t, "http", netguard.New(false)).Execute(context.Background(), httpMonitor(t, "http", cfg), nil)
	assert.Equal(t, shared.MonitorStatusUp, off.Status, off.Message)
	assert.Equal(t, int32(1), tokenRequests.Load())
}

func TestTCPExecutor_BlocksPrivateTargets(t *testing.T) {
	address, accepted := silentServer(t)

	blocked := guardedExecutor(t, "tcp", netguard.New(true)).Execute(context.Background(), tcpMonitor(t, address), nil)
	requireBlocked(t, blocked)

	localhost := tcpMonitor(t, address)
	_, port, _ := net.SplitHostPort(address)
	localhost.Config = fmt.Sprintf(`{"host": "localhost", "port": %s}`, port)
	requireBlocked(t, guardedExecutor(t, "tcp", netguard.New(true)).Execute(context.Background(), localhost, nil))
	assert.Zero(t, accepted(), "the server saw no connection")

	off := guardedExecutor(t, "tcp", netguard.New(false)).Execute(context.Background(), tcpMonitor(t, address), nil)
	assert.Equal(t, shared.MonitorStatusUp, off.Status, off.Message)
}

func TestPingExecutor_BlocksPrivateTargets(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "localhost", "169.254.169.254", "10.1.2.3", "::1"} {
		t.Run(host, func(t *testing.T) {
			monitor := &Monitor{ID: "ping", Type: "ping", Timeout: 2, Config: fmt.Sprintf(`{"host": %q}`, host)}

			result := guardedExecutor(t, "ping", netguard.New(true)).Execute(context.Background(), monitor, nil)

			requireBlocked(t, result)
		})
	}
}

func TestPingExecutor_PingsTheAddressItChecked(t *testing.T) {
	p := NewPingExecutor(zap.NewNop().Sugar())
	p.useGuard(netguard.New(true))

	host, err := p.guardedHost(context.Background(), "8.8.8.8")
	require.NoError(t, err)
	assert.Equal(t, "8.8.8.8", host, "the check pings the literal that was checked and doesn't look the name up again")

	_, err = p.guardedHost(context.Background(), "nonexistent.invalid")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), blockedMessage)
	assert.Contains(t, err.Error(), "failed to resolve host")

	off := NewPingExecutor(zap.NewNop().Sugar())
	host, err = off.guardedHost(context.Background(), "localhost")
	require.NoError(t, err)
	assert.Equal(t, "localhost", host, "off, the name goes on as it was")
}

func TestPreferIPv4(t *testing.T) {
	addrs := []netip.Addr{netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("::ffff:192.0.2.1"), netip.MustParseAddr("198.51.100.1")}

	assert.Equal(t, netip.MustParseAddr("192.0.2.1"), preferIPv4(addrs))
	assert.Equal(t, netip.MustParseAddr("2001:db8::1"), preferIPv4(addrs[:1]))
}

func TestDNSExecutor_BlocksAResolverAtAPrivateAddress(t *testing.T) {
	port := startDNSServer(t)

	for _, resolveType := range []string{"A", "TXT", "CAA", "SOA"} {
		t.Run(resolveType, func(t *testing.T) {
			monitor := dnsMonitor(port, "peekaping.test", resolveType, 2)

			blocked := guardedExecutor(t, "dns", netguard.New(true)).Execute(context.Background(), monitor, nil)
			requireBlocked(t, blocked)

			off := guardedExecutor(t, "dns", netguard.New(false)).Execute(context.Background(), monitor, nil)
			assert.Equal(t, shared.MonitorStatusUp, off.Status, off.Message)
		})
	}
}

func TestSMTPExecutor_BlocksPrivateTargets(t *testing.T) {
	listener, cleanup := setupMockSMTPServer(t, false, false, false)
	defer cleanup()
	monitor := &Monitor{ID: "smtp", Type: "smtp", Timeout: 2, Config: fmt.Sprintf(`{"host": "127.0.0.1", "port": %d}`, listener.Addr().(*net.TCPAddr).Port)}

	blocked := guardedExecutor(t, "smtp", netguard.New(true)).Execute(context.Background(), monitor, nil)
	requireBlocked(t, blocked)

	off := guardedExecutor(t, "smtp", netguard.New(false)).Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusUp, off.Status, off.Message)
}

// A probe turns its guard on after the executors are built
func TestExecutors_FollowAGuardTurnedOnLater(t *testing.T) {
	guard := netguard.New(false)
	e := guardedExecutor(t, "tcp", guard)
	address, _ := silentServer(t)

	before := e.Execute(context.Background(), tcpMonitor(t, address), nil)
	guard.Set(true)
	after := e.Execute(context.Background(), tcpMonitor(t, address), nil)

	assert.Equal(t, shared.MonitorStatusUp, before.Status, before.Message)
	requireBlocked(t, after)
}

// A check builds its transport when it starts, and a probe's guard can be turned on while it runs,
// at a hello. The transport asks the guard at each dial, so the request and any redirect it makes
// after that are refused.
func TestHTTPTransports_FollowAGuardTurnedOnAfterTheyAreBuilt(t *testing.T) {
	server, requests := countingServer(t, nil)

	t.Run("the check's transport", func(t *testing.T) {
		guard := netguard.New(false)
		transport := buildGuardedTransport(&http.Transport{}, nil, guard)
		guard.Set(true)

		_, err := (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get(server.URL)

		require.ErrorIs(t, err, netguard.ErrBlocked)
	})

	t.Run("a SOCKS proxy's", func(t *testing.T) {
		address, accepted := silentServer(t)
		host, port, err := net.SplitHostPort(address)
		require.NoError(t, err)
		portNumber, _ := strconv.Atoi(port)
		guard := netguard.New(false)
		transport := buildGuardedTransport(&http.Transport{}, &Proxy{Protocol: "socks5", Host: host, Port: portNumber}, guard)
		guard.Set(true)

		_, err = (&http.Client{Transport: transport, Timeout: 2 * time.Second}).Get("http://target.invalid/")

		require.ErrorIs(t, err, netguard.ErrBlocked)
		assert.Zero(t, accepted(), "the proxy saw no connection")
	})

	t.Run("a token request's client", func(t *testing.T) {
		guard := netguard.New(false)
		client, closeIdle := guardedClient(guard)
		defer closeIdle()
		guard.Set(true)

		_, err := client.Get(server.URL)

		require.ErrorIs(t, err, netguard.ErrBlocked)
	})

	assert.Zero(t, requests.Load(), "the server saw no request")
}

func TestExecutorRegistry_RefusesBlockedLiteralTargets(t *testing.T) {
	registry := NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(netguard.New(true)))
	httpConfig := func(url string) string {
		return fmt.Sprintf(`{"url": %q, "method": "GET", "encoding": "json", "accepted_statuscodes": ["2XX"], "authMethod": "none"}`, url)
	}
	oauthConfig := `{"url": "https://example.com", "method": "GET", "encoding": "json", "accepted_statuscodes": ["2XX"],
		"authMethod": "oauth2-cc", "oauth_auth_method": "client_secret_post", "oauth_token_url": "http://169.254.169.254/token",
		"oauth_client_id": "id", "oauth_client_secret": "secret"}`
	tests := []struct {
		name, monitorType, config string
		blocked                   string
	}{
		{"http url", "http", httpConfig("http://127.0.0.1:8080/health"), "127.0.0.1"},
		{"http url on the metadata address", "http-keyword", httpConfig("http://169.254.169.254/"), "169.254.169.254"},
		{"http url on a private IPv6 address", "http", httpConfig("http://[fd00::1]/"), "fd00::1"},
		{"http oauth token url", "http", oauthConfig, "169.254.169.254"},
		{"tcp host", "tcp", `{"host": "10.0.0.5", "port": 22}`, "10.0.0.5"},
		{"ping host", "ping", `{"host": "192.168.1.1"}`, "192.168.1.1"},
		{"dns resolver", "dns", `{"host": "example.com", "resolver_server": "127.0.0.1", "port": 53, "resolve_type": "A"}`, "127.0.0.1"},
		{"smtp host", "smtp", `{"host": "100.64.0.1", "port": 25}`, "100.64.0.1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.ValidateConfig(tc.monitorType, tc.config)

			var target *netguard.TargetError
			require.ErrorAs(t, err, &target)
			assert.Equal(t, tc.blocked, target.Host)
			assert.ErrorIs(t, err, netguard.ErrBlocked)
		})
	}

	t.Run("names and public addresses pass", func(t *testing.T) {
		assert.NoError(t, registry.ValidateConfig("http", httpConfig("https://example.com/health")))
		assert.NoError(t, registry.ValidateConfig("http", httpConfig("http://localhost:8080/")))
		assert.NoError(t, registry.ValidateConfig("http", httpConfig("http://8.8.8.8/")))
		assert.NoError(t, registry.ValidateConfig("tcp", `{"host": "internal.corp.example", "port": 22}`))
		assert.NoError(t, registry.ValidateConfig("ping", `{"host": "[2606:4700:4700::1111]"}`))
	})

	t.Run("an invalid config is still invalid", func(t *testing.T) {
		err := registry.ValidateConfig("tcp", `{"host": "10.0.0.5"}`)
		require.Error(t, err)
		assert.NotErrorIs(t, err, netguard.ErrBlocked)
	})

	t.Run("a type that dials nothing", func(t *testing.T) {
		assert.NoError(t, registry.ValidateConfig("push", `{"pushToken": "abcdefghijklmnopqrstuvwxyz012345"}`))
	})

	t.Run("off, the same targets are saved", func(t *testing.T) {
		for _, guard := range []*netguard.Guard{nil, netguard.New(false)} {
			off := NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(guard))
			assert.NoError(t, off.ValidateConfig("http", httpConfig("http://127.0.0.1:8080/health")))
			assert.NoError(t, off.ValidateConfig("tcp", `{"host": "10.0.0.5", "port": 22}`))
		}
	})
}

func TestKnownTypes_DoNotDependOnTheGuard(t *testing.T) {
	assert.Equal(t, KnownTypes(), NewExecutorRegistry(zap.NewNop().Sugar(), WithGuard(netguard.New(true))).Types())
}

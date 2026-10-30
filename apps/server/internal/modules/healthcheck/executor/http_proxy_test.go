package executor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildProxyTransport_HTTPProxyURL(t *testing.T) {
	tests := []struct {
		name  string
		proxy Proxy
		want  string
	}{
		{name: "http", proxy: Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080}, want: "http://proxy.example.com:8080"},
		{name: "https", proxy: Proxy{Protocol: "https", Host: "proxy.example.com", Port: 8443}, want: "https://proxy.example.com:8443"},
		{name: "default protocol", proxy: Proxy{Host: "proxy.example.com", Port: 3128}, want: "http://proxy.example.com:3128"},
		{name: "with credentials", proxy: Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "user", Password: "pass"}, want: "http://user:pass@proxy.example.com:8080"},
		{name: "IPv4", proxy: Proxy{Protocol: "http", Host: "192.0.2.1", Port: 8080}, want: "http://192.0.2.1:8080"},
		{name: "bare IPv6", proxy: Proxy{Protocol: "http", Host: "2001:db8::1", Port: 8080}, want: "http://[2001:db8::1]:8080"},
		{name: "bracketed IPv6", proxy: Proxy{Protocol: "http", Host: "[2001:db8::1]", Port: 8080}, want: "http://[2001:db8::1]:8080"},
		{name: "bracketed IPv6 with credentials", proxy: Proxy{Protocol: "https", Host: "[2001:db8::1]", Port: 8443, Auth: true, Username: "user", Password: "pass"}, want: "https://user:pass@[2001:db8::1]:8443"},
		{name: "credentials are escaped", proxy: Proxy{Protocol: "https", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "us er", Password: "p@ss"}, want: "https://us%20er:p%40ss@proxy.example.com:8080"},
		{name: "credentials without auth", proxy: Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080, Username: "user", Password: "pass"}, want: "http://proxy.example.com:8080"},
		{name: "auth without a username", proxy: Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Password: "pass"}, want: "http://proxy.example.com:8080"},
		{name: "auth without a password", proxy: Proxy{Protocol: "http", Host: "proxy.example.com", Port: 8080, Auth: true, Username: "user"}, want: "http://proxy.example.com:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := buildProxyTransport(&http.Transport{}, &tt.proxy).(*http.Transport)
			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)

			proxyURL, err := transport.Proxy(req)

			require.NoError(t, err)
			assert.Equal(t, tt.want, proxyURL.String())
		})
	}
}

func TestBuildProxyTransport_ProtocolsThatSetNoHTTPProxy(t *testing.T) {
	t.Run("socks drops the HTTP proxy and dials through the proxy", func(t *testing.T) {
		base := &http.Transport{Proxy: http.ProxyFromEnvironment}

		result := buildProxyTransport(base, &Proxy{Protocol: "socks5", Host: "proxy.example.com", Port: 1080}).(*http.Transport)

		assert.Nil(t, result.Proxy)
		assert.NotNil(t, result.DialContext)
	})

	t.Run("an unsupported protocol leaves the transport as it was", func(t *testing.T) {
		base := &http.Transport{Proxy: http.ProxyFromEnvironment}

		result := buildProxyTransport(base, &Proxy{Protocol: "ftp", Host: "proxy.example.com", Port: 21}).(*http.Transport)

		assert.NotNil(t, result.Proxy)
		assert.Nil(t, result.DialContext)
	})
}

// socksGreeting is what a client sends a SOCKS5 server when it connects: the methods it offers,
// and the credentials it then sends if the server picks the username and password method.
type socksGreeting struct {
	methods  []byte
	user     string
	password string
}

// startSOCKSGreetingServer accepts one connection to a SOCKS5 proxy and reports the client's
// greeting, then turns the client away: nothing is proxied.
func startSOCKSGreetingServer(t *testing.T) (string, <-chan socksGreeting) {
	t.Helper()
	return startSOCKSGreetingServerOn(t, "127.0.0.1:0")
}

// startSOCKSGreetingServerOn is startSOCKSGreetingServer listening on addr
func startSOCKSGreetingServerOn(t *testing.T, addr string) (string, <-chan socksGreeting) {
	t.Helper()
	lis, err := net.Listen("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	greetings := make(chan socksGreeting, 1)

	go func() {
		conn, err := lis.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		header := make([]byte, 2)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		greeting := socksGreeting{methods: make([]byte, header[1])}
		if _, err := io.ReadFull(conn, greeting.methods); err != nil {
			return
		}

		const userPassword = 0x02
		if !containsByte(greeting.methods, userPassword) {
			_, _ = conn.Write([]byte{0x05, 0xff})
			greetings <- greeting
			return
		}
		_, _ = conn.Write([]byte{0x05, userPassword})

		// RFC 1929: version, user length, user, password length, password
		var length [1]byte
		if _, err := io.ReadFull(conn, length[:1]); err != nil { // version
			return
		}
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return
		}
		user := make([]byte, length[0])
		if _, err := io.ReadFull(conn, user); err != nil {
			return
		}
		if _, err := io.ReadFull(conn, length[:]); err != nil {
			return
		}
		password := make([]byte, length[0])
		if _, err := io.ReadFull(conn, password); err != nil {
			return
		}
		greeting.user, greeting.password = string(user), string(password)
		_, _ = conn.Write([]byte{0x01, 0x01})
		greetings <- greeting
	}()

	return lis.Addr().String(), greetings
}

// TestBuildProxyTransport_SOCKSIPv6Host checks that a SOCKS proxy on an IPv6 address gets the
// client's greeting whether the host is saved bare or in brackets
func TestBuildProxyTransport_SOCKSIPv6Host(t *testing.T) {
	requireIPv6Loopback(t)
	for _, form := range []string{"%s", "[%s]"} {
		t.Run(form, func(t *testing.T) {
			addr, greetings := startSOCKSGreetingServerOn(t, "[::1]:0")
			host, port, err := net.SplitHostPort(addr)
			require.NoError(t, err)
			portNumber, err := net.LookupPort("tcp", port)
			require.NoError(t, err)
			proxy := &Proxy{Protocol: "socks5", Host: fmt.Sprintf(form, host), Port: portNumber}
			transport := buildProxyTransport(&http.Transport{}, proxy).(*http.Transport)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, err = transport.DialContext(ctx, "tcp", "target.example.com:80")

			require.Error(t, err)
			select {
			case greeting := <-greetings:
				assert.Equal(t, []byte{0x00}, greeting.methods)
			case <-time.After(5 * time.Second):
				t.Fatalf("the proxy got no greeting: %v", err)
			}
		})
	}
}

func containsByte(bytes []byte, b byte) bool {
	for _, candidate := range bytes {
		if candidate == b {
			return true
		}
	}
	return false
}

func TestBuildProxyTransport_SOCKSAuth(t *testing.T) {
	const noAuth, userPassword = 0x00, 0x02

	tests := []struct {
		name         string
		protocol     string
		auth         bool
		user         string
		password     string
		wantMethods  []byte
		wantUser     string
		wantPassword string
	}{
		{name: "no auth", protocol: "socks5", wantMethods: []byte{noAuth}},
		{name: "credentials without auth", protocol: "socks5", user: "user", password: "pass", wantMethods: []byte{noAuth}},
		{name: "auth without a username", protocol: "socks5", auth: true, password: "pass", wantMethods: []byte{noAuth}},
		{name: "auth without a password", protocol: "socks5", auth: true, user: "user", wantMethods: []byte{noAuth}},
		{name: "auth with credentials", protocol: "socks5", auth: true, user: "user", password: "pass", wantMethods: []byte{noAuth, userPassword}, wantUser: "user", wantPassword: "pass"},
		{name: "socks4 protocol", protocol: "socks4", auth: true, user: "u", password: "p", wantMethods: []byte{noAuth, userPassword}, wantUser: "u", wantPassword: "p"},
		{name: "socks5h protocol", protocol: "socks5h", wantMethods: []byte{noAuth}},
		{name: "socks protocol", protocol: "socks", wantMethods: []byte{noAuth}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, greetings := startSOCKSGreetingServer(t)
			host, port, err := net.SplitHostPort(addr)
			require.NoError(t, err)
			portNumber, err := net.LookupPort("tcp", port)
			require.NoError(t, err)
			proxy := &Proxy{Protocol: tt.protocol, Host: host, Port: portNumber, Auth: tt.auth, Username: tt.user, Password: tt.password}
			transport := buildProxyTransport(&http.Transport{}, proxy).(*http.Transport)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := transport.DialContext(ctx, "tcp", "target.example.com:80")

			require.Error(t, err)
			assert.Nil(t, conn)
			select {
			case greeting := <-greetings:
				assert.Equal(t, socksGreeting{methods: tt.wantMethods, user: tt.wantUser, password: tt.wantPassword}, greeting)
			case <-time.After(5 * time.Second):
				t.Fatal("the proxy got no greeting")
			}
		})
	}
}

package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"peekaping/internal/httpx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// request is a request from remote, with the forwarding headers given.
func request(remote string, forwardedFor []string, realIP string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for _, value := range forwardedFor {
		r.Header.Add("X-Forwarded-For", value)
	}
	if realIP != "" {
		r.Header.Set("X-Real-IP", realIP)
	}
	return r
}

func TestTrustedProxies_ClientIP(t *testing.T) {
	const trusted = "10.0.0.0/8, 127.0.0.1, ::1, fc00::/7"

	for _, tc := range []struct {
		name         string
		trusted      string
		remote       string
		forwardedFor []string
		realIP       string
		want         string
	}{
		// Nobody is trusted, or the connection is from outside the trusted ranges: the
		// connection's address is the client, whatever the headers say.
		{"no trusted proxies, no headers", "", "192.0.2.1:1234", nil, "", "192.0.2.1"},
		{"no trusted proxies, forged headers", "", "192.0.2.1:1234", []string{"198.51.100.1"}, "198.51.100.2", "192.0.2.1"},
		{"untrusted peer, forged headers", trusted, "192.0.2.1:1234", []string{"10.0.0.9"}, "10.0.0.8", "192.0.2.1"},
		{"untrusted peer next to a trusted range", trusted, "11.0.0.1:1234", []string{"198.51.100.1"}, "", "11.0.0.1"},
		{"untrusted IPv6 peer, forged headers", trusted, "[2001:db8::1]:1234", []string{"198.51.100.1"}, "198.51.100.2", "2001:db8::1"},

		// A trusted peer: the client is the rightmost X-Forwarded-For entry that isn't a trusted proxy.
		{"trusted peer, one entry", trusted, "10.0.0.1:1234", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"trusted peer, the client forged the leftmost entry", trusted, "10.0.0.1:1234", []string{"203.0.113.9, 198.51.100.7"}, "", "198.51.100.7"},
		{"trusted peer, a chain of trusted proxies", trusted, "10.0.0.1:1234", []string{"203.0.113.9, 198.51.100.7, 10.0.0.3, 10.0.0.2"}, "", "198.51.100.7"},
		{"trusted peer, the header arrives as several lines", trusted, "10.0.0.1:1234", []string{"203.0.113.9", "198.51.100.7, 10.0.0.2"}, "", "198.51.100.7"},
		{"trusted peer, spaces around the entries", trusted, "10.0.0.1:1234", []string{"  198.51.100.7 ,10.0.0.2  "}, "", "198.51.100.7"},
		{"trusted peer, garbage left of the client", trusted, "10.0.0.1:1234", []string{"garbage, 198.51.100.7"}, "", "198.51.100.7"},
		{"trusted peer, every entry is a trusted proxy", trusted, "10.0.0.1:1234", []string{"10.0.0.5, 10.0.0.3, 10.0.0.2"}, "", "10.0.0.5"},
		{"trusted peer, an IPv4-mapped client address", trusted, "10.0.0.1:1234", []string{"::ffff:198.51.100.7"}, "", "198.51.100.7"},
		{"trusted IPv4-mapped peer", trusted, "[::ffff:10.0.0.1]:1234", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"trusted loopback peer", trusted, "127.0.0.1:1234", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"trusted peer with IPv6 entries", trusted, "[fc00::1]:1234", []string{"2001:db8::7, fc00::2"}, "", "2001:db8::7"},
		{"a single address as a trusted entry", "192.0.2.1", "192.0.2.1:1234", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"an IPv4-mapped range is the IPv4 range", "::ffff:10.0.0.0/104", "10.0.0.1:1234", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"an IPv4-mapped range doesn't trust outside it", "::ffff:10.0.0.0/104", "11.0.0.1:1234", []string{"198.51.100.7"}, "", "11.0.0.1"},
		{"a single address doesn't trust its neighbour", "192.0.2.1", "192.0.2.2:1234", []string{"198.51.100.7"}, "", "192.0.2.2"},

		// A trusted peer with no X-Forwarded-For at all: X-Real-IP, then the peer.
		{"trusted peer, no headers", trusted, "10.0.0.1:1234", nil, "", "10.0.0.1"},
		{"trusted peer, X-Real-IP only", trusted, "10.0.0.1:1234", nil, "198.51.100.9", "198.51.100.9"},
		{"trusted peer, X-Forwarded-For wins over X-Real-IP", trusted, "10.0.0.1:1234", []string{"198.51.100.7"}, "198.51.100.9", "198.51.100.7"},
		{"trusted peer, X-Real-IP is garbage", trusted, "10.0.0.1:1234", nil, "garbage", "10.0.0.1"},

		// A trusted peer whose X-Forwarded-For can't be resolved: the peer, never X-Real-IP, which the
		// client may have sent itself when the proxy doesn't set it.
		{"trusted peer, X-Forwarded-For is garbage", trusted, "10.0.0.1:1234", []string{"garbage"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, X-Forwarded-For has a port", trusted, "10.0.0.1:1234", []string{"1.2.3.4:5"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, X-Forwarded-For has a bracketed IPv6 address", trusted, "10.0.0.1:1234", []string{"[2001:db8::1]"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, X-Forwarded-For has a zone", trusted, "10.0.0.1:1234", []string{"fe80::1%eth0"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, X-Forwarded-For is unknown", trusted, "10.0.0.1:1234", []string{"unknown"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, X-Forwarded-For is empty", trusted, "10.0.0.1:1234", []string{""}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, a malformed entry before any client", trusted, "10.0.0.1:1234", []string{"198.51.100.7, , 10.0.0.2"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, a trusted proxy's entry has a port", trusted, "10.0.0.1:1234", []string{"198.51.100.7, 10.0.0.2:80"}, "9.9.9.9", "10.0.0.1"},
		{"trusted peer, both headers are garbage", trusted, "10.0.0.1:1234", []string{"garbage"}, "garbage", "10.0.0.1"},
		{"trusted peer, X-Real-IP is a chain", trusted, "10.0.0.1:1234", nil, "198.51.100.9, 10.0.0.2", "10.0.0.1"},

		// A request with no usable connection address has no client.
		{"no port in the connection address", trusted, "invalid", []string{"198.51.100.1"}, "", ""},
		{"no trusted proxies, no port", "", "invalid", nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxies, err := httpx.ParseTrustedProxies(tc.trusted)
			require.NoError(t, err)
			assert.Equal(t, tc.want, proxies.ClientIP(request(tc.remote, tc.forwardedFor, tc.realIP)))
		})
	}
}

func TestTrustedProxies_NilTrustsNobody(t *testing.T) {
	var proxies *httpx.TrustedProxies
	assert.Equal(t, "192.0.2.1", proxies.ClientIP(request("192.0.2.1:1234", []string{"198.51.100.1"}, "198.51.100.2")))
}

func TestParseTrustedProxies_ExplainsAShortIPv4MappedRange(t *testing.T) {
	_, err := httpx.ParseTrustedProxies("::ffff:0:0/80")

	assert.EqualError(t, err, `invalid TRUSTED_PROXIES entry "::ffff:0:0/80": an IPv4-mapped IPv6 range must be /96 or longer, because addresses are matched as IPv4`)
}

func TestParseTrustedProxies(t *testing.T) {
	for _, valid := range []string{
		"",
		"   ",
		"127.0.0.1",
		"::1",
		"10.0.0.0/8",
		"fc00::/7",
		" 127.0.0.1 ,10.0.0.0/8,  ::1/128 ",
		"10.0.0.0/8,",
		"::ffff:10.0.0.0/104",
		"::ffff:0:0/96",
	} {
		_, err := httpx.ParseTrustedProxies(valid)
		assert.NoError(t, err, "%q", valid)
	}

	for _, tc := range []struct{ list, entry string }{
		{"localhost", "localhost"},
		{"*", "*"},
		{"10.0.0.0/33", "10.0.0.0/33"},
		{"::ffff:0:0/80", "::ffff:0:0/80"},
		{"10.0.0.0/8, ::ffff:10.0.0.0/95", "::ffff:10.0.0.0/95"},
		{"10.0.0.256", "10.0.0.256"},
		{"10.0.0.1/", "10.0.0.1/"},
		{"127.0.0.1, 10.0.0.0/8, garbage", "garbage"},
		{"127.0.0.1 10.0.0.1", "127.0.0.1 10.0.0.1"},
	} {
		_, err := httpx.ParseTrustedProxies(tc.list)
		require.Error(t, err, "%q", tc.list)
		assert.ErrorContains(t, err, "TRUSTED_PROXIES", "%q", tc.list)
		assert.ErrorContains(t, err, `"`+tc.entry+`"`, "%q", tc.list)
	}
}

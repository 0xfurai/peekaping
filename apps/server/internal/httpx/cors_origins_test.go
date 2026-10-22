package httpx_test

import (
	"testing"

	"peekaping/internal/httpx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCORSOrigins(t *testing.T) {
	for _, tc := range []struct {
		name      string
		list      string
		clientURL string
		allowed   []string // each is sent back as Access-Control-Allow-Origin
		denied    []string
	}{
		{
			name:      "an unset list allows the origin of CLIENT_URL",
			clientURL: "http://localhost:8383",
			allowed:   []string{"http://localhost:8383"},
			denied:    []string{"http://localhost:3000", "http://localhost", "https://localhost:8383", "http://evil.example"},
		},
		{
			name:      "an empty or blank list is an unset one",
			list:      " , ,",
			clientURL: "https://peek.example",
			allowed:   []string{"https://peek.example"},
			denied:    []string{"https://evil.example"},
		},
		{
			name:      "CLIENT_URL's path, trailing slash, query and userinfo are dropped",
			clientURL: "https://user:pass@Peek.Example:8443/status/?tab=1#top",
			allowed:   []string{"https://peek.example:8443"},
			denied:    []string{"https://peek.example", "https://peek.example:8443/status/", "https://user@peek.example:8443"},
		},
		{
			name:      "a list replaces the origin of CLIENT_URL",
			list:      "https://a.example,https://b.example:8443",
			clientURL: "https://peek.example",
			allowed:   []string{"https://a.example", "https://b.example:8443"},
			denied:    []string{"https://peek.example", "https://b.example", "https://a.example:8443", "https://c.example"},
		},
		{
			name:      "entries are trimmed, and empty ones between commas are ignored",
			list:      "  https://a.example ,, https://b.example ,",
			clientURL: "https://peek.example",
			allowed:   []string{"https://a.example", "https://b.example"},
			denied:    []string{"https://peek.example"},
		},
		{
			name:      "scheme and host are lowercased, a trailing slash is dropped, and the port is kept as given",
			list:      "HTTPS://Client.Example/,http://localhost:80,https://[::1]:8443",
			clientURL: "https://peek.example",
			allowed:   []string{"https://client.example", "http://localhost:80", "https://[::1]:8443"},
			denied:    []string{"https://Client.Example", "http://localhost", "https://client.example/", "https://[::1]"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origins, err := httpx.ParseCORSOrigins(tc.list, tc.clientURL)
			require.NoError(t, err)

			for _, origin := range tc.allowed {
				assert.Equal(t, origin, origins.AllowOrigin(origin), origin)
			}
			for _, origin := range tc.denied {
				assert.Empty(t, origins.AllowOrigin(origin), origin)
			}
		})
	}
}

func TestParseCORSOrigins_AStarAllowsAnyOrigin(t *testing.T) {
	for _, list := range []string{"*", " * ,"} {
		origins, err := httpx.ParseCORSOrigins(list, "https://peek.example")

		require.NoError(t, err, list)
		for _, origin := range []string{"https://peek.example", "https://evil.example", "http://localhost:8383"} {
			assert.Equal(t, "*", origins.AllowOrigin(origin), origin)
		}
	}
}

func TestCORSOrigins_NilAndZeroAllowNobody(t *testing.T) {
	var none *httpx.CORSOrigins
	assert.Empty(t, none.AllowOrigin("https://client.example"))
	assert.Empty(t, (&httpx.CORSOrigins{}).AllowOrigin("https://client.example"))
}

func TestParseCORSOrigins_Refuses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		list      string
		clientURL string
		wantErr   string
	}{
		{"an entry with a path", "http://x.example/path", "https://peek.example", `invalid CORS_ORIGINS entry "http://x.example/path"`},
		{"a bare host", "https://ok.example,example.com", "https://peek.example", `invalid CORS_ORIGINS entry "example.com"`},
		{"a host and port with no scheme", "localhost:8383", "https://peek.example", `invalid CORS_ORIGINS entry "localhost:8383"`},
		{"a scheme that isn't http or https", "ftp://x.example", "https://peek.example", `invalid CORS_ORIGINS entry "ftp://x.example"`},
		{"a scheme and no host", "https://", "https://peek.example", `invalid CORS_ORIGINS entry "https://"`},
		{"a query", "https://x.example?a=1", "https://peek.example", `invalid CORS_ORIGINS entry "https://x.example?a=1"`},
		{"a fragment", "https://x.example#top", "https://peek.example", `invalid CORS_ORIGINS entry "https://x.example#top"`},
		{"userinfo", "https://user@x.example", "https://peek.example", `invalid CORS_ORIGINS entry "https://user@x.example"`},
		{"a port that isn't a number", "https://x.example:http", "https://peek.example", `invalid CORS_ORIGINS entry "https://x.example:http"`},
		{"a star mixed with other entries", "https://a.example,*", "https://peek.example", `invalid CORS_ORIGINS entry "*"`},
		{"two stars", "*,*", "https://peek.example", `invalid CORS_ORIGINS entry "*"`},
		{"a CLIENT_URL that is no origin when the list is unset", "", "peek.example", `invalid CLIENT_URL "peek.example"`},
		{"an empty CLIENT_URL when the list is unset", "", "", `invalid CLIENT_URL ""`},
		{"a CLIENT_URL that isn't http or https when the list is unset", "", "ftp://peek.example", `invalid CLIENT_URL "ftp://peek.example"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origins, err := httpx.ParseCORSOrigins(tc.list, tc.clientURL)

			assert.Nil(t, origins)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestParseCORSOrigins_IgnoresAnInvalidClientURLWhenTheListIsSet(t *testing.T) {
	origins, err := httpx.ParseCORSOrigins("https://a.example", "not a url")

	require.NoError(t, err)
	assert.Equal(t, "https://a.example", origins.AllowOrigin("https://a.example"))
}

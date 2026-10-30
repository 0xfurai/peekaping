package executor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/modules/shared"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func executeHTTP(t *testing.T, monitorType string, cfg HTTPConfig) *Result {
	t.Helper()
	return NewHTTPExecutor(zap.NewNop().Sugar()).Execute(context.Background(), httpMonitor(t, monitorType, cfg), nil)
}

func TestHTTPExecutor_Execute_RequestErrors(t *testing.T) {
	t.Run("a URL that can't be requested", func(t *testing.T) {
		result := executeHTTP(t, "http", HTTPConfig{Url: "http://example.com/%zz", AuthMethod: "none"})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, `parse "http://example.com/%zz": invalid URL escape "%zz"`, result.Message)
		assert.False(t, result.StartTime.IsZero())
		assert.False(t, result.EndTime.Before(result.StartTime))
	})

	t.Run("headers that aren't JSON", func(t *testing.T) {
		result := executeHTTP(t, "http", HTTPConfig{Url: "http://example.com", AuthMethod: "none", Headers: "{bad"})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Equal(t, "invalid headers json: invalid character 'b' looking for beginning of object key string", result.Message)
	})
}

func TestHTTPExecutor_Execute_OAuth2TokenFailures(t *testing.T) {
	tests := []struct {
		name         string
		tokenHandler http.HandlerFunc
		tokenURL     func(serverURL string) string
		wantMessage  string
		wantPrefix   string
	}{
		{
			name:         "token request can't be created",
			tokenHandler: func(w http.ResponseWriter, r *http.Request) {},
			tokenURL:     func(string) string { return "http://example.com/%zz" },
			wantMessage:  `failed to create oauth2 token request: parse "http://example.com/%zz": invalid URL escape "%zz"`,
		},
		{
			name:         "token endpoint unreachable",
			tokenHandler: func(w http.ResponseWriter, r *http.Request) {},
			tokenURL:     func(string) string { return "http://" + closedAddr(t) + "/token" },
			wantPrefix:   "failed to get oauth2 token: ",
		},
		{
			name:         "token endpoint refuses",
			tokenHandler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			tokenURL:     func(serverURL string) string { return serverURL + "/token" },
			wantMessage:  "oauth2 token endpoint returned status: 500",
		},
		{
			name:         "token response isn't JSON",
			tokenHandler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) },
			tokenURL:     func(serverURL string) string { return serverURL + "/token" },
			wantPrefix:   "failed to parse oauth2 token response: invalid character",
		},
		{
			name:         "token response has no token",
			tokenHandler: func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
			tokenURL:     func(serverURL string) string { return serverURL + "/token" },
			wantMessage:  "failed to parse oauth2 token response: %!w(<nil>)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenServer := httptest.NewServer(tt.tokenHandler)
			defer tokenServer.Close()

			result := executeHTTP(t, "http", HTTPConfig{
				Url:               "http://example.com",
				AuthMethod:        "oauth2-cc",
				OauthAuthMethod:   "client_secret_post",
				OauthTokenUrl:     tt.tokenURL(tokenServer.URL),
				OauthClientId:     "id",
				OauthClientSecret: "secret",
			})

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			if tt.wantMessage != "" {
				assert.Equal(t, tt.wantMessage, result.Message)
			} else {
				assert.True(t, strings.HasPrefix(result.Message, tt.wantPrefix), result.Message)
			}
		})
	}
}

func TestHTTPExecutor_Execute_MTLSInvalidCA(t *testing.T) {
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))

	result := executeHTTP(t, "http", HTTPConfig{
		Url:        "https://example.com",
		AuthMethod: "mtls",
		TlsCert:    pki.clientCertPEM,
		TlsKey:     pki.clientKeyPEM,
		TlsCa:      "not a certificate",
	})

	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Equal(t, "invalid mTLS CA cert", result.Message)
}

func TestHTTPExecutor_Execute_NTLMFallsBackToBasicWithTheDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		wantUser string
	}{
		{name: "with a domain", domain: "CORP", wantUser: `CORP\user`},
		{name: "without a domain", wantUser: "user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var attempts []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, password, hasAuth := r.BasicAuth()
				mu.Lock()
				if hasAuth {
					attempts = append(attempts, user+":"+password)
				} else {
					attempts = append(attempts, "anonymous")
				}
				mu.Unlock()
				if !hasAuth {
					w.WriteHeader(http.StatusUnauthorized)
				}
			}))
			defer server.Close()

			result := executeHTTP(t, "http", HTTPConfig{
				Url:             server.URL,
				AuthMethod:      "ntlm",
				BasicAuthUser:   "user",
				BasicAuthPass:   "pass",
				AuthDomain:      tt.domain,
				AuthWorkstation: "WS",
			})

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, []string{"anonymous", tt.wantUser + ":pass"}, attempts)
		})
	}
}

func TestHTTPExecutor_Execute_ResponseOutcomes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		case "/truncated":
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 10\r\n\r\nabc")
			_ = buf.Flush()
			_ = conn.Close()
		default:
			_, _ = w.Write([]byte(`{"a": 1, "name": "peekaping"}`))
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	tests := []struct {
		name        string
		monitorType string
		path        string
		edit        func(*HTTPConfig)
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{name: "up", path: "/", wantStatus: shared.MonitorStatusUp, wantMessage: "200 - 200 OK"},
		{name: "status not accepted", path: "/error", wantStatus: shared.MonitorStatusDown, wantMessage: "HTTP request failed with status: 500"},
		{name: "body can't be read", path: "/truncated", wantStatus: shared.MonitorStatusDown, wantMessage: "Failed to read response body: unexpected EOF"},
		{name: "keyword found", path: "/", edit: func(c *HTTPConfig) { c.Keyword = "peekaping" }, wantStatus: shared.MonitorStatusUp, wantMessage: "200 - 200 OK"},
		{name: "keyword missing", path: "/", edit: func(c *HTTPConfig) { c.Keyword = "absent" }, wantStatus: shared.MonitorStatusDown, wantMessage: "Keyword check failed: keyword 'absent' not found in response"},
		{name: "inverted keyword present", path: "/", edit: func(c *HTTPConfig) { c.Keyword, c.InvertKeyword = "peekaping", true }, wantStatus: shared.MonitorStatusDown, wantMessage: "Keyword check failed: keyword 'peekaping' found in response (expected absent)"},
		{name: "inverted keyword absent", path: "/", edit: func(c *HTTPConfig) { c.Keyword, c.InvertKeyword = "absent", true }, wantStatus: shared.MonitorStatusUp, wantMessage: "200 - 200 OK"},
		{name: "keyword is not checked on a failed status", path: "/error", edit: func(c *HTTPConfig) { c.Keyword = "x" }, wantStatus: shared.MonitorStatusDown, wantMessage: "HTTP request failed with status: 500"},
		{
			name: "json query holds", monitorType: "http-json-query", path: "/",
			edit:       func(c *HTTPConfig) { c.JsonQuery, c.JsonCondition, c.ExpectedValue = "a", "==", "1" },
			wantStatus: shared.MonitorStatusUp, wantMessage: "200 - 200 OK",
		},
		{
			name: "json query fails", monitorType: "http-json-query", path: "/",
			edit:       func(c *HTTPConfig) { c.JsonQuery, c.JsonCondition, c.ExpectedValue = "a", "!=", "1" },
			wantStatus: shared.MonitorStatusDown, wantMessage: "JSON query validation failed: query 'a' with condition '!=' and expected value '1'",
		},
		{
			name: "json query fails with the default condition", monitorType: "http-json-query", path: "/",
			edit:       func(c *HTTPConfig) { c.JsonQuery, c.ExpectedValue = "a", "2" },
			wantStatus: shared.MonitorStatusDown, wantMessage: "JSON query validation failed: query 'a' with condition '==' and expected value '2'",
		},
		{
			name: "json query path is missing", monitorType: "http-json-query", path: "/",
			edit:       func(c *HTTPConfig) { c.JsonQuery, c.JsonCondition, c.ExpectedValue = "b", "==", "1" },
			wantStatus: shared.MonitorStatusDown, wantMessage: "JSON query validation error: JSON query path not found: b",
		},
		{
			name: "json query is ignored for a plain http monitor", monitorType: "http", path: "/",
			edit:       func(c *HTTPConfig) { c.JsonQuery, c.JsonCondition, c.ExpectedValue = "a", "==", "2" },
			wantStatus: shared.MonitorStatusUp, wantMessage: "200 - 200 OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := HTTPConfig{Url: server.URL + tt.path, AuthMethod: "none"}
			if tt.edit != nil {
				tt.edit(&cfg)
			}
			monitorType := tt.monitorType
			if monitorType == "" {
				monitorType = "http"
			}

			result := executeHTTP(t, monitorType, cfg)

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Equal(t, tt.wantMessage, result.Message)
			assert.Nil(t, result.TLSInfo, "a plain HTTP check has no TLS information")
			assert.False(t, result.EndTime.Before(result.StartTime))
		})
	}
}

func TestHTTPExecutor_Execute_ReportsTLSInformationOnFailures(t *testing.T) {
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))
	server := startTLSServer(t, pki, false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))

	tests := []struct {
		name        string
		cfg         HTTPConfig
		wantMessage string
	}{
		{name: "status not accepted", cfg: HTTPConfig{Url: server.URL + "/error", AuthMethod: "none", IgnoreTlsErrors: true}, wantMessage: "HTTP request failed with status: 500"},
		{
			name:        "request fails after the handshake",
			cfg:         HTTPConfig{Url: server.URL + "/loop", AuthMethod: "none", IgnoreTlsErrors: true, MaxRedirects: 1},
			wantMessage: "too many redirects: followed 2 redirects, maximum allowed is 1",
		},
		{
			name: "request fails after the handshake, with a client certificate",
			cfg: HTTPConfig{
				Url: server.URL + "/loop", AuthMethod: "mtls", TlsCert: pki.clientCertPEM, TlsKey: pki.clientKeyPEM, TlsCa: pki.caPEM, MaxRedirects: 1,
			},
			wantMessage: "too many redirects: followed 2 redirects, maximum allowed is 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executeHTTP(t, "http", tt.cfg)

			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, tt.wantMessage)
			require.NotNil(t, result.TLSInfo)
		})
	}

	t.Run("no handshake, no information", func(t *testing.T) {
		result := executeHTTP(t, "http", HTTPConfig{Url: "https://" + closedAddr(t), AuthMethod: "none"})

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Nil(t, result.TLSInfo)
	})
}

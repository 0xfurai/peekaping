package executor

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"peekaping/internal/modules/shared"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPExecutor_Validate(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid http config",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: false,
		},
		{
			name: "invalid url",
			config: `{
				"url": "not-a-url",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: true,
		},
		{
			name: "invalid method",
			config: `{
				"url": "http://example.com",
				"method": "INVALID",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: true,
		},
		{
			name: "invalid encoding",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "invalid",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: true,
		},
		{
			name: "invalid status codes",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["INVALID"],
				"authMethod": "none"
			}`,
			expectedError: true,
		},
		{
			name: "valid basic auth config",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "basic",
				"basic_auth_user": "user",
				"basic_auth_pass": "pass"
			}`,
			expectedError: false,
		},
		{
			name: "invalid basic auth config - missing credentials",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "basic"
			}`,
			expectedError: true,
		},
		{
			name: "valid ntlm auth config",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "ntlm",
				"basic_auth_user": "user",
				"basic_auth_pass": "pass",
				"authDomain": "domain",
				"authWorkstation": "workstation"
			}`,
			expectedError: false,
		},
		{
			name: "invalid ntlm auth config - missing domain",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "ntlm",
				"basic_auth_user": "user",
				"basic_auth_pass": "pass",
				"authWorkstation": "workstation"
			}`,
			expectedError: true,
		},
		{
			name: "valid oauth2-cc config with client_secret_basic",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "client_secret_basic",
				"oauth_token_url": "http://auth.example.com/token",
				"oauth_client_id": "client123",
				"oauth_client_secret": "secret456",
				"oauth_scopes": "read write"
			}`,
			expectedError: false,
		},
		{
			name: "valid oauth2-cc config with client_secret_post",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "client_secret_post",
				"oauth_token_url": "http://auth.example.com/token",
				"oauth_client_id": "client123",
				"oauth_client_secret": "secret456"
			}`,
			expectedError: false,
		},
		{
			name: "invalid oauth2-cc config - missing token url",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "client_secret_basic",
				"oauth_client_id": "client123",
				"oauth_client_secret": "secret456"
			}`,
			expectedError: true,
		},
		{
			name: "invalid oauth2-cc config - invalid auth method",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "invalid_method",
				"oauth_token_url": "http://auth.example.com/token",
				"oauth_client_id": "client123",
				"oauth_client_secret": "secret456"
			}`,
			expectedError: true,
		},
		{
			name: "valid mtls config",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "mtls",
				"tlsCert": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
				"tlsKey": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
				"tlsCa": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----"
			}`,
			expectedError: false, // Validation only checks structure, not cert validity
		},
		{
			name: "valid mtls config without CA",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "mtls",
				"tlsCert": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
				"tlsKey": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----"
			}`,
			expectedError: false,
		},
		{
			name: "invalid mtls config - missing cert",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "mtls",
				"tlsKey": "-----BEGIN PRIVATE KEY-----\nMIIE...\n-----END PRIVATE KEY-----",
				"tlsCa": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----"
			}`,
			expectedError: true,
		},
		{
			name: "invalid mtls config - missing key",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "mtls",
				"tlsCert": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----",
				"tlsCa": "-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----"
			}`,
			expectedError: true,
		},
		{
			name: "valid config with max redirects",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"max_redirects": 5
			}`,
			expectedError: false,
		},
		{
			name: "invalid config with negative max redirects",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"max_redirects": -1
			}`,
			expectedError: true,
		},
		{
			name: "valid config with headers",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"headers": "{\"X-Custom-Header\": \"value\"}"
			}`,
			expectedError: false,
		},
		{
			name: "invalid config with malformed headers json",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"headers": "{invalid json}"
			}`,
			expectedError: true,
		},
		{
			name: "valid xml encoding with xml body",
			config: `{
				"url": "http://example.com",
				"method": "POST",
				"encoding": "xml",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"body": "<root><test>value</test></root>"
			}`,
			expectedError: false,
		},
		{
			name: "invalid xml encoding with malformed xml body",
			config: `{
				"url": "http://example.com",
				"method": "POST",
				"encoding": "xml",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"body": "<root><test>value</test>"
			}`,
			expectedError: true,
		},
		{
			name: "valid form encoding with form body",
			config: `{
				"url": "http://example.com",
				"method": "POST",
				"encoding": "form",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"body": "field1=value1&field2=value2"
			}`,
			expectedError: false,
		},
		{
			name: "invalid form encoding with malformed form body",
			config: `{
				"url": "http://example.com",
				"method": "POST",
				"encoding": "form",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"body": "field1=value1&field2=%ZZ"
			}`,
			expectedError: true,
		},
		{
			name: "valid text encoding with text body",
			config: `{
				"url": "http://example.com",
				"method": "POST",
				"encoding": "text",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"body": "plain text content"
			}`,
			expectedError: false,
		},
		{
			name: "valid config with ignore_tls_errors true",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"ignore_tls_errors": true
			}`,
			expectedError: false,
		},
		{
			name: "valid config with ignore_tls_errors false",
			config: `{
				"url": "https://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"ignore_tls_errors": false
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition equals",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "",
				"json_condition": "==",
				"expected_value": "test"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition not equals",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "",
				"json_condition": "!=",
				"expected_value": "test"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition greater than",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "value",
				"json_condition": ">",
				"expected_value": "10"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition less than",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "value",
				"json_condition": "<",
				"expected_value": "10"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition greater than or equal",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "value",
				"json_condition": ">=",
				"expected_value": "10"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_condition less than or equal",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "value",
				"json_condition": "<=",
				"expected_value": "10"
			}`,
			expectedError: false,
		},
		{
			name: "invalid json_condition",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "value",
				"json_condition": "invalid",
				"expected_value": "test"
			}`,
			expectedError: true,
		},
		{
			name: "valid json_query gjson path",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "userId",
				"json_condition": "==",
				"expected_value": "1"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_query nested path",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "user.id",
				"json_condition": "==",
				"expected_value": "1"
			}`,
			expectedError: false,
		},
		{
			name: "valid json_query empty for full response",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"json_query": "",
				"json_condition": "==",
				"expected_value": "full_response"
			}`,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executor.Validate(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHTTPExecutor_Unmarshal(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	tests := []struct {
		name          string
		config        string
		expectedError bool
	}{
		{
			name: "valid config",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedError: false,
		},
		{
			name:          "invalid json",
			config:        `{invalid json}`,
			expectedError: true,
		},
		{
			name:          "empty string",
			config:        "",
			expectedError: true,
		},
		{
			name: "config with unknown fields",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none",
				"unknownField": "value"
			}`,
			expectedError: true, // DisallowUnknownFields is set
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Unmarshal(tt.config)
			if tt.expectedError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				cfg, ok := result.(*HTTPConfig)
				assert.True(t, ok)
				assert.NotNil(t, cfg)

			}
		})
	}
}

func TestHTTPExecutor_Execute(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > 0 {
			// A request with a body carries the encoding's Content-Type.
			if r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		} else if r.Header.Get("Content-Type") != "" {
			// A bodyless request must not carry one (issue #395).
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	tests := []struct {
		name           string
		monitor        *Monitor
		config         string
		expectedStatus shared.MonitorStatus
	}{
		{
			name: "successful GET request",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"url": "` + server.URL + `",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedStatus: shared.MonitorStatusUp,
		},
		{
			name: "successful POST request with body",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"url": "` + server.URL + `",
				"method": "POST",
				"encoding": "json",
				"body": "{\"test\": \"value\"}",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			expectedStatus: shared.MonitorStatusUp,
		},
		{
			name: "failed request with invalid status code",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
			config: `{
				"url": "` + server.URL + `",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["3XX"],
				"authMethod": "none"
			}`,
			expectedStatus: shared.MonitorStatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
		})
	}
}

func TestHTTPExecutor_Execute_ContentTypeFollowsTheBody(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())

	tests := []struct {
		name            string
		method          string
		encoding        string
		body            string
		headers         string
		wantContentType string
	}{
		{
			name:     "bodyless GET sends none",
			method:   "GET",
			encoding: "json",
		},
		{
			name:     "bodyless HEAD sends none",
			method:   "HEAD",
			encoding: "json",
		},
		{
			name:     "bodyless POST sends none",
			method:   "POST",
			encoding: "json",
		},
		{
			name:     "bodyless PUT with form encoding sends none",
			method:   "PUT",
			encoding: "form",
		},
		{
			name:     "bodyless DELETE with xml encoding sends none",
			method:   "DELETE",
			encoding: "xml",
		},
		{
			name:     "bodyless GET drops the legacy web default",
			method:   "GET",
			encoding: "json",
			headers:  `{"Content-Type":"application/json"}`,
		},
		{
			name:            "bodyless GET keeps an explicit content type",
			method:          "GET",
			encoding:        "json",
			headers:         `{"Content-Type":"application/vnd.api+json"}`,
			wantContentType: "application/vnd.api+json",
		},
		{
			name:            "bodyless GET keeps an explicitly cased content type",
			method:          "GET",
			encoding:        "json",
			headers:         `{"content-type":"application/json"}`,
			wantContentType: "application/json",
		},
		{
			name:            "bodyless GET keeps an explicitly padded content type",
			method:          "GET",
			encoding:        "json",
			headers:         `{"Content-Type":" application/json "}`,
			wantContentType: "application/json",
		},
		{
			name:     "bodyless GET with Accept sends no content type",
			method:   "GET",
			encoding: "text",
			headers:  `{"Accept":"application/json"}`,
		},
		{
			name:     "bodyless JSON GET with other headers sends no content type",
			method:   "GET",
			encoding: "json",
			headers:  `{"Accept":"application/json","X-Trace":"1"}`,
		},
		{
			name:            "body sends the encoding content type",
			method:          "POST",
			encoding:        "xml",
			body:            "<root/>",
			wantContentType: "application/xml",
		},
		{
			name:            "body infers content type with other headers",
			method:          "POST",
			encoding:        "json",
			body:            `{"key":"value"}`,
			headers:         `{"Accept":"application/json"}`,
			wantContentType: "application/json",
		},
		{
			name:            "body keeps an explicit content type",
			method:          "POST",
			encoding:        "json",
			body:            `{"key":"value"}`,
			headers:         `{"Content-Type":"application/vnd.api+json"}`,
			wantContentType: "application/vnd.api+json",
		},
	}

	for _, monitorType := range []string{"http", "http-keyword", "http-json-query"} {
		for _, tt := range tests {
			t.Run(monitorType+" "+tt.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if got := r.Header.Get("Content-Type"); got != tt.wantContentType {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusOK)
				}))
				defer server.Close()

				cfg := HTTPConfig{
					Url:                 server.URL,
					Method:              tt.method,
					Encoding:            tt.encoding,
					Body:                tt.body,
					Headers:             tt.headers,
					AcceptedStatusCodes: []string{"2XX"},
					AuthMethod:          "none",
				}
				config, err := json.Marshal(cfg)
				require.NoError(t, err)

				monitor := &Monitor{
					ID:       "monitor1",
					Type:     monitorType,
					Name:     "Test Monitor",
					Interval: 30,
					Timeout:  5,
					Config:   string(config),
				}

				result := executor.Execute(context.Background(), monitor, nil)
				assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			})
		}
	}
}

// httpMonitor builds a monitor that GETs cfg.Url and accepts 2XX responses.
func httpMonitor(t *testing.T, monitorType string, cfg HTTPConfig) *Monitor {
	t.Helper()
	cfg.Method, cfg.Encoding, cfg.AcceptedStatusCodes = "GET", "json", []string{"2XX"}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &Monitor{ID: "monitor1", Type: monitorType, Name: "Test Monitor", Timeout: 5, Config: string(b)}
}

func TestHTTPExecutor_Execute_BasicAuth(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "user" || pass != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tests := []struct {
		name        string
		cfg         HTTPConfig
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{
			name:        "valid credentials",
			cfg:         HTTPConfig{AuthMethod: "basic", BasicAuthUser: "user", BasicAuthPass: "pass"},
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "200 - 200 OK",
		},
		{
			name:        "wrong credentials",
			cfg:         HTTPConfig{AuthMethod: "basic", BasicAuthUser: "user", BasicAuthPass: "wrong"},
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "HTTP request failed with status: 401",
		},
		{
			name:        "no credentials",
			cfg:         HTTPConfig{AuthMethod: "none"},
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "HTTP request failed with status: 401",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Url = server.URL
			result := executor.Execute(context.Background(), httpMonitor(t, "http", tt.cfg), nil)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}

func TestHTTPExecutor_Execute_Keyword(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body>All systems operational</body></html>"))
	}))
	defer server.Close()

	tests := []struct {
		name        string
		keyword     string
		invert      bool
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{
			name:        "keyword present",
			keyword:     "operational",
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "200 - 200 OK",
		},
		{
			name:        "keyword absent",
			keyword:     "outage",
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "Keyword check failed: keyword 'outage' not found in response",
		},
		{
			name:        "inverted keyword absent",
			keyword:     "outage",
			invert:      true,
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "200 - 200 OK",
		},
		{
			name:        "inverted keyword present",
			keyword:     "operational",
			invert:      true,
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "Keyword check failed: keyword 'operational' found in response (expected absent)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := httpMonitor(t, "http-keyword", HTTPConfig{
				Url:           server.URL,
				AuthMethod:    "none",
				Keyword:       tt.keyword,
				InvertKeyword: tt.invert,
			})

			result := executor.Execute(context.Background(), monitor, nil)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}

func TestHTTPExecutor_Execute_JsonQuery(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"queue": {"depth": 12}}`))
	}))
	defer server.Close()

	tests := []struct {
		name        string
		query       string
		condition   string
		expected    string
		wantStatus  shared.MonitorStatus
		wantMessage string
	}{
		{
			name:        "condition holds",
			query:       "queue.depth",
			condition:   "<",
			expected:    "100",
			wantStatus:  shared.MonitorStatusUp,
			wantMessage: "200 - 200 OK",
		},
		{
			name:        "condition fails",
			query:       "queue.depth",
			condition:   ">",
			expected:    "100",
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "JSON query validation failed: query 'queue.depth' with condition '>' and expected value '100'",
		},
		{
			name:        "path missing",
			query:       "queue.size",
			condition:   "==",
			expected:    "0",
			wantStatus:  shared.MonitorStatusDown,
			wantMessage: "JSON query validation error: JSON query path not found: queue.size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := httpMonitor(t, "http-json-query", HTTPConfig{
				Url:           server.URL,
				AuthMethod:    "none",
				JsonQuery:     tt.query,
				JsonCondition: tt.condition,
				ExpectedValue: tt.expected,
			})

			result := executor.Execute(context.Background(), monitor, nil)
			assert.Equal(t, tt.wantStatus, result.Status)
			assert.Equal(t, tt.wantMessage, result.Message)
		})
	}
}

func TestHTTPExecutor_Execute_DifferentEncodings(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that checks content types
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedContentTypes := map[string]string{
			"json": "application/json",
			"form": "application/x-www-form-urlencoded",
			"xml":  "application/xml",
			"text": "text/plain",
		}

		// Extract encoding from URL path for testing
		encoding := strings.TrimPrefix(r.URL.Path, "/")
		expectedType, exists := expectedContentTypes[encoding]
		if !exists {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Header.Get("Content-Type") != expectedType {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tests := []struct {
		encoding string
		body     string
	}{
		{"json", `{"key": "value"}`},
		{"form", "key=value&key2=value2"},
		{"xml", "<root><key>value</key></root>"},
		{"text", "plain text content"},
	}

	for _, tt := range tests {
		t.Run("encoding_"+tt.encoding, func(t *testing.T) {
			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"url": "` + server.URL + `/` + tt.encoding + `",
					"method": "POST",
					"encoding": "` + tt.encoding + `",
					"body": "` + strings.ReplaceAll(tt.body, `"`, `\"`) + `",
					"accepted_statuscodes": ["2XX"],
					"authMethod": "none"
				}`,
			}

			result := executor.Execute(context.Background(), monitor, nil)
			assert.Equal(t, shared.MonitorStatusUp, result.Status)
		})
	}
}

func TestHTTPExecutor_Execute_WithHeaders(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that checks custom headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Header.Get("X-Another-Header") != "another-value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"headers": "{\"X-Custom-Header\": \"custom-value\", \"X-Another-Header\": \"another-value\"}",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none"
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusUp, result.Status)
}

func TestHTTPExecutor_Execute_InvalidConfig(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	tests := []struct {
		name        string
		config      string
		monitor     *Monitor
		wantMessage string
	}{
		{
			name:        "invalid JSON config",
			config:      `{invalid json}`,
			wantMessage: "failed to parse config",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
		},
		{
			name: "invalid headers JSON",
			config: `{
				"url": "http://example.com",
				"method": "GET",
				"encoding": "json",
				"headers": "{invalid json}",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			wantMessage: "invalid headers json",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
		},
		{
			name: "invalid URL in config",
			config: `{
				"url": "not a valid url",
				"method": "GET",
				"encoding": "json",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "none"
			}`,
			wantMessage: "unsupported protocol scheme",
			monitor: &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.monitor.Config = tt.config
			result := executor.Execute(context.Background(), tt.monitor, nil)
			assert.Equal(t, shared.MonitorStatusDown, result.Status)
			assert.Contains(t, result.Message, tt.wantMessage)
		})
	}
}

func TestHTTPExecutor_Execute_OAuth2(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create OAuth2 token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Parse form data
		err := r.ParseForm()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if r.Form.Get("grant_type") != "client_credentials" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Check for Basic auth or form-based auth
		authHeader := r.Header.Get("Authorization")
		clientId := r.Form.Get("client_id")
		clientSecret := r.Form.Get("client_secret")

		// Reject request if more than one authentication method is used (RFC 6749 §2.3)
		if authHeader != "" && (clientId != "" || clientSecret != "") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if authHeader == "" && (clientId == "" || clientSecret == "") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"access_token": "test-token", "token_type": "Bearer"}`))
	}))
	defer tokenServer.Close()

	// Create target server that checks Bearer token
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	tests := []struct {
		name           string
		authMethod     string
		expectedStatus shared.MonitorStatus
	}{
		{
			name:           "oauth2 with client_secret_basic",
			authMethod:     "client_secret_basic",
			expectedStatus: shared.MonitorStatusUp,
		},
		{
			name:           "oauth2 with client_secret_post",
			authMethod:     "client_secret_post",
			expectedStatus: shared.MonitorStatusUp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
				Config: `{
					"url": "` + targetServer.URL + `",
					"method": "GET",
					"encoding": "json",
					"accepted_statuscodes": ["2XX"],
					"authMethod": "oauth2-cc",
					"oauth_auth_method": "` + tt.authMethod + `",
					"oauth_token_url": "` + tokenServer.URL + `/token",
					"oauth_client_id": "test-client",
					"oauth_client_secret": "test-secret",
					"oauth_scopes": "read write"
				}`,
			}

			result := executor.Execute(context.Background(), monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)
		})
	}
}

func TestHTTPExecutor_Execute_OAuth2_Failures(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create failing OAuth2 token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer tokenServer.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "http://example.com",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "oauth2-cc",
			"oauth_auth_method": "client_secret_basic",
			"oauth_token_url": "` + tokenServer.URL + `/token",
			"oauth_client_id": "test-client",
			"oauth_client_secret": "test-secret"
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "oauth2 token endpoint returned status")
}

func TestHTTPExecutor_Execute_OAuth2_CredentialsPlacement(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	type capturedReq struct {
		authHeader string
		basicUser  string
		basicPass  string
		basicOK    bool
		clientID   string
		secret     string
		grantType  string
		scope      string
	}
	captured := make(chan capturedReq, 2)

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		user, pass, ok := r.BasicAuth()
		captured <- capturedReq{
			authHeader: r.Header.Get("Authorization"),
			basicUser:  user,
			basicPass:  pass,
			basicOK:    ok,
			clientID:   r.Form.Get("client_id"),
			secret:     r.Form.Get("client_secret"),
			grantType:  r.Form.Get("grant_type"),
			scope:      r.Form.Get("scope"),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token": "token-xyz"}`))
	}))
	defer tokenServer.Close()

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer targetServer.Close()

	t.Run("client_secret_basic sends credentials in Authorization header only", func(t *testing.T) {
		monitor := &Monitor{
			ID:       "m-basic",
			Type:     "http",
			Interval: 30,
			Timeout:  5,
			Config: fmt.Sprintf(`{
				"url": %q,
				"method": "GET",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "client_secret_basic",
				"oauth_token_url": %q,
				"oauth_client_id": "my-client",
				"oauth_client_secret": "my-secret",
				"oauth_scopes": "read write"
			}`, targetServer.URL, tokenServer.URL+"/token"),
		}

		result := executor.Execute(context.Background(), monitor, nil)
		assert.Equal(t, shared.MonitorStatusUp, result.Status)

		select {
		case req := <-captured:
			assert.True(t, req.basicOK, "Basic auth header should be present")
			assert.Equal(t, "my-client", req.basicUser)
			assert.Equal(t, "my-secret", req.basicPass)
			assert.Empty(t, req.clientID, "client_id should not be in form body for client_secret_basic")
			assert.Empty(t, req.secret, "client_secret should not be in form body for client_secret_basic")
			assert.Equal(t, "client_credentials", req.grantType)
			assert.Equal(t, "read write", req.scope)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for token request")
		}
	})

	t.Run("client_secret_post sends credentials in form body only", func(t *testing.T) {
		monitor := &Monitor{
			ID:       "m-post",
			Type:     "http",
			Interval: 30,
			Timeout:  5,
			Config: fmt.Sprintf(`{
				"url": %q,
				"method": "GET",
				"accepted_statuscodes": ["2XX"],
				"authMethod": "oauth2-cc",
				"oauth_auth_method": "client_secret_post",
				"oauth_token_url": %q,
				"oauth_client_id": "my-client",
				"oauth_client_secret": "my-secret",
				"oauth_scopes": "read write"
			}`, targetServer.URL, tokenServer.URL+"/token"),
		}

		result := executor.Execute(context.Background(), monitor, nil)
		assert.Equal(t, shared.MonitorStatusUp, result.Status)

		select {
		case req := <-captured:
			assert.False(t, req.basicOK, "Basic auth should not be set for client_secret_post")
			assert.Empty(t, req.authHeader, "Authorization header should not be set for client_secret_post")
			assert.Equal(t, "my-client", req.clientID)
			assert.Equal(t, "my-secret", req.secret)
			assert.Equal(t, "client_credentials", req.grantType)
			assert.Equal(t, "read write", req.scope)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for token request")
		}
	})
}

func TestHTTPExecutor_Execute_Timeout(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  1, // 1 second timeout
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none"
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	// Accept both context deadline exceeded and client timeout messages
	timeoutOccurred := strings.Contains(result.Message, "context deadline exceeded") ||
		strings.Contains(result.Message, "Client.Timeout exceeded")
	assert.True(t, timeoutOccurred, "Expected timeout error, got: %s", result.Message)
}

func TestHTTPExecutor_Execute_Proxy(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none"
		}`,
	}

	proxy := &Proxy{
		ID:       "proxy1",
		Host:     "127.0.0.1",
		Port:     closedPort(t),
		Protocol: "http",
		Auth:     true,
		Username: "proxyuser",
		Password: "proxypass",
	}

	result := executor.Execute(context.Background(), monitor, proxy)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "connection refused")
}

func TestHTTPExecutor_Execute_MaxRedirects(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that redirects using query parameters to track count
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectParam := r.URL.Query().Get("redirect")
		var redirectCount int
		if redirectParam != "" {
			var err error
			redirectCount, err = strconv.Atoi(redirectParam)
			if err != nil {
				redirectCount = 0
			}
		}

		if redirectCount < 3 {
			nextCount := redirectCount + 1
			redirectURL := fmt.Sprintf("http://%s?redirect=%d", r.Host, nextCount)
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none",
			"max_redirects": 2
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	// Should fail because we exceed max redirects (2) but need 3 redirects to reach final page
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "too many redirects")
	assert.Contains(t, result.Message, "maximum allowed is 2")
}

func TestHTTPExecutor_Execute_MaxRedirects_Success(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that redirects using query parameters to track count
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectParam := r.URL.Query().Get("redirect")
		var redirectCount int
		if redirectParam != "" {
			var err error
			redirectCount, err = strconv.Atoi(redirectParam)
			if err != nil {
				redirectCount = 0
			}
		}

		if redirectCount < 2 { // Only 2 redirects, within limit of 5
			nextCount := redirectCount + 1
			redirectURL := fmt.Sprintf("http://%s?redirect=%d", r.Host, nextCount)
			http.Redirect(w, r, redirectURL, http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none",
			"max_redirects": 5
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	// Should succeed because we only have 2 redirects within the limit of 5
	assert.Equal(t, shared.MonitorStatusUp, result.Status)
}

func TestHTTPExecutor_Execute_DisabledRedirects(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create test server that redirects
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String()+"?redirected", http.StatusFound)
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none",
			"max_redirects": 0
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	// Should fail because redirects are disabled
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "redirects disabled")
}

func TestIsStatusAccepted(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		acceptedCodes  []string
		expectedResult bool
	}{
		{"200 with 2XX", 200, []string{"2XX"}, true},
		{"299 with 2XX", 299, []string{"2XX"}, true},
		{"300 with 2XX", 300, []string{"2XX"}, false},
		{"404 with 4XX", 404, []string{"4XX"}, true},
		{"500 with 5XX", 500, []string{"5XX"}, true},
		{"200 with 3XX,4XX", 200, []string{"3XX", "4XX"}, false},
		{"404 with 3XX,4XX", 404, []string{"3XX", "4XX"}, true},
		{"302 with 3XX", 302, []string{"3XX"}, true},
		{"100 with 2XX", 100, []string{"2XX"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isStatusAccepted(tt.statusCode, tt.acceptedCodes)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestBuildProxyTransport(t *testing.T) {
	base := &http.Transport{}

	t.Run("nil proxy", func(t *testing.T) {
		result := buildProxyTransport(base, nil)
		assert.Equal(t, base, result)
	})

	t.Run("http proxy", func(t *testing.T) {
		proxy := &Proxy{
			Host:     "proxy.example.com",
			Port:     8080,
			Protocol: "http",
			Auth:     false,
		}
		result := buildProxyTransport(base, proxy)
		assert.NotNil(t, result)
		transport := result.(*http.Transport)
		assert.NotNil(t, transport.Proxy)
	})

	t.Run("http proxy with auth", func(t *testing.T) {
		proxy := &Proxy{
			Host:     "proxy.example.com",
			Port:     8080,
			Protocol: "http",
			Auth:     true,
			Username: "user",
			Password: "pass",
		}
		result := buildProxyTransport(base, proxy)
		assert.NotNil(t, result)
		transport := result.(*http.Transport)
		assert.NotNil(t, transport.Proxy)
	})

	t.Run("http proxy on an IPv6 host", func(t *testing.T) {
		proxy := &Proxy{Host: "2001:db8::1", Port: 8080, Protocol: "http"}
		transport := buildProxyTransport(base, proxy).(*http.Transport)
		req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
		require.NoError(t, err)

		proxyURL, err := transport.Proxy(req)

		require.NoError(t, err)
		assert.Equal(t, "http://[2001:db8::1]:8080", proxyURL.String())
	})

	t.Run("socks5 proxy", func(t *testing.T) {
		proxy := &Proxy{
			Host:     "proxy.example.com",
			Port:     1080,
			Protocol: "socks5",
			Auth:     false,
		}
		result := buildProxyTransport(base, proxy)
		assert.NotNil(t, result)
		transport := result.(*http.Transport)
		assert.NotNil(t, transport.DialContext)
	})

	t.Run("socks5 proxy with auth", func(t *testing.T) {
		proxy := &Proxy{
			Host:     "proxy.example.com",
			Port:     1080,
			Protocol: "socks5",
			Auth:     true,
			Username: "user",
			Password: "pass",
		}
		result := buildProxyTransport(base, proxy)
		assert.NotNil(t, result)
		transport := result.(*http.Transport)
		assert.NotNil(t, transport.DialContext)
	})

	t.Run("default protocol", func(t *testing.T) {
		proxy := &Proxy{
			Host: "proxy.example.com",
			Port: 8080,
			// No protocol specified, should default to "http"
		}
		result := buildProxyTransport(base, proxy)
		assert.NotNil(t, result)
		transport := result.(*http.Transport)
		assert.NotNil(t, transport.Proxy)
	})

	t.Run("unsupported protocol", func(t *testing.T) {
		proxy := &Proxy{
			Host:     "proxy.example.com",
			Port:     8080,
			Protocol: "unsupported",
		}
		result := buildProxyTransport(base, proxy)
		assert.Equal(t, base, result)
	})
}

func TestHTTPExecutor_Execute_MTLS_InvalidCert(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "https://example.com",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "mtls",
			"tlsCert": "invalid-cert",
			"tlsKey": "invalid-key",
			"tlsCa": "invalid-ca"
		}`,
	}

	result := executor.Execute(context.Background(), monitor, nil)
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid mTLS cert/key")
}

// testPKI holds PEM-encoded test certificates: a CA, a server certificate
// for 127.0.0.1 and a client certificate, both signed by the CA.
type testPKI struct {
	caPEM, serverCertPEM, serverKeyPEM, clientCertPEM, clientKeyPEM string
}

// generateTestCerts creates a testPKI whose server certificate expires at serverNotAfter.
func generateTestCerts(t *testing.T, serverNotAfter time.Time) testPKI {
	t.Helper()
	encode := func(blockType string, der []byte) string {
		return string(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
	}

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Peekaping Test CA"},
		NotBefore:             time.Now().Add(-365 * 24 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	issue := func(template *x509.Certificate) (certPEM, keyPEM string) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		template.KeyUsage = x509.KeyUsageDigitalSignature
		der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
		require.NoError(t, err)
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)
		return encode("CERTIFICATE", der), encode("PRIVATE KEY", keyDER)
	}

	pki := testPKI{caPEM: encode("CERTIFICATE", caDER)}
	pki.serverCertPEM, pki.serverKeyPEM = issue(&x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    serverNotAfter.Add(-90 * 24 * time.Hour),
		NotAfter:     serverNotAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	pki.clientCertPEM, pki.clientKeyPEM = issue(&x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "peekaping"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return pki
}

// startTLSServer serves handler with the PKI's server certificate. With
// requireClientCert, it only accepts clients with a certificate from the PKI's CA.
func startTLSServer(t *testing.T, pki testPKI, requireClientCert bool, handler http.Handler) *httptest.Server {
	t.Helper()
	cert, err := tls.X509KeyPair([]byte(pki.serverCertPEM), []byte(pki.serverKeyPEM))
	require.NoError(t, err)
	server := httptest.NewUnstartedServer(handler)
	// Tests that expect a failed handshake would otherwise log it.
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	if requireClientCert {
		clientCAs := x509.NewCertPool()
		require.True(t, clientCAs.AppendCertsFromPEM([]byte(pki.caPEM)))
		server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
		server.TLS.ClientCAs = clientCAs
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestHTTPExecutor_Execute_ReportsServerCertificate(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	tests := []struct {
		name              string
		notAfter          time.Time
		wantDaysRemaining int
	}{
		// Each expiry is an hour past whole days so DaysRemaining doesn't
		// truncate to the next day while the test runs.
		{name: "expires in 10 days", notAfter: time.Now().Add(10*24*time.Hour + time.Hour), wantDaysRemaining: 10},
		{name: "expired 2 days ago", notAfter: time.Now().Add(-2*24*time.Hour - time.Hour), wantDaysRemaining: -2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notAfter := tt.notAfter.UTC().Truncate(time.Second)
			server := startTLSServer(t, generateTestCerts(t, notAfter), false, okHandler)

			result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
				Url:             server.URL,
				AuthMethod:      "none",
				IgnoreTlsErrors: true,
			}), nil)

			assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
			require.NotNil(t, result.TLSInfo)
			assert.False(t, result.TLSInfo.Valid, "the chain wasn't verified")
			cert := result.TLSInfo.CertInfo
			require.NotNil(t, cert)
			assert.Equal(t, "CN=127.0.0.1", cert.Subject)
			assert.Equal(t, "CN=Peekaping Test CA", cert.Issuer)
			assert.WithinDuration(t, notAfter, cert.ValidTo, 0)
			assert.Equal(t, tt.wantDaysRemaining, cert.DaysRemaining)
		})
	}
}

func TestHTTPExecutor_Execute_MTLS(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())
	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))
	// The server already refuses clients without a certificate from its CA. The
	// handler also answers 403 unless the certificate it saw is the monitor's.
	server := startTLSServer(t, pki, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) == 0 || r.TLS.PeerCertificates[0].Subject.CommonName != "peekaping" {
			w.WriteHeader(http.StatusForbidden)
		}
	}))

	t.Run("with a client certificate", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:        server.URL,
			AuthMethod: "mtls",
			TlsCert:    pki.clientCertPEM,
			TlsKey:     pki.clientKeyPEM,
			TlsCa:      pki.caPEM,
		}), nil)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		require.NotNil(t, result.TLSInfo)
		assert.True(t, result.TLSInfo.Valid, "the chain verifies against the configured CA")
	})

	t.Run("without a CA, a server that demands a client certificate is Up", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:        server.URL,
			AuthMethod: "mtls",
			TlsCert:    pki.clientCertPEM,
			TlsKey:     pki.clientKeyPEM,
			// The server's CA isn't in the system roots, so skip verifying it
			// to test the client certificate on its own.
			IgnoreTlsErrors: true,
		}), nil)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	})

	t.Run("with a whitespace-only CA, it counts as no CA", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:             server.URL,
			AuthMethod:      "mtls",
			TlsCert:         pki.clientCertPEM,
			TlsKey:          pki.clientKeyPEM,
			TlsCa:           "  \n",
			IgnoreTlsErrors: true,
		}), nil)

		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
	})

	t.Run("without a CA, a server certificate from an unknown CA is Down", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:        server.URL,
			AuthMethod: "mtls",
			TlsCert:    pki.clientCertPEM,
			TlsKey:     pki.clientKeyPEM,
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "x509:")
	})

	t.Run("with an invalid CA", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:        server.URL,
			AuthMethod: "mtls",
			TlsCert:    pki.clientCertPEM,
			TlsKey:     pki.clientKeyPEM,
			TlsCa:      "not a certificate",
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "invalid mTLS CA cert")
	})

	t.Run("with a CA that did not sign the server certificate, it is Down", func(t *testing.T) {
		otherPKI := generateTestCerts(t, time.Now().Add(24*time.Hour))

		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:        server.URL,
			AuthMethod: "mtls",
			TlsCert:    pki.clientCertPEM,
			TlsKey:     pki.clientKeyPEM,
			TlsCa:      otherPKI.caPEM,
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "x509:")
	})

	t.Run("without a client certificate", func(t *testing.T) {
		result := executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
			Url:             server.URL,
			AuthMethod:      "none",
			IgnoreTlsErrors: true,
		}), nil)

		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "certificate required")
	})
}

// With no CA, an mTLS monitor trusts the system's roots; with a CA, it trusts
// only that CA. Go reads SSL_CERT_FILE once per process, so the test runs itself
// again with the server's CA as the only system root, the way the standard
// library's own tests do.
func TestHTTPExecutor_Execute_MTLS_SystemRoots(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Go reads SSL_CERT_FILE only on Linux; elsewhere it uses the OS trust store")
	}

	if serverURL := os.Getenv("PEEKAPING_TEST_MTLS_URL"); serverURL != "" {
		executor := NewHTTPExecutor(zap.NewNop().Sugar())
		check := func(tlsCa string) *Result {
			return executor.Execute(context.Background(), httpMonitor(t, "http", HTTPConfig{
				Url:        serverURL,
				AuthMethod: "mtls",
				TlsCert:    os.Getenv("PEEKAPING_TEST_MTLS_CERT"),
				TlsKey:     os.Getenv("PEEKAPING_TEST_MTLS_KEY"),
				TlsCa:      tlsCa,
			}), nil)
		}

		result := check("")
		assert.Equal(t, shared.MonitorStatusUp, result.Status, result.Message)
		require.NotNil(t, result.TLSInfo)
		assert.True(t, result.TLSInfo.Valid, "the chain verifies against the system roots")

		// The server's CA is a system root here, so a CA that didn't sign
		// the server certificate is Down only if the system roots are ignored.
		result = check(os.Getenv("PEEKAPING_TEST_MTLS_OTHER_CA"))
		assert.Equal(t, shared.MonitorStatusDown, result.Status)
		assert.Contains(t, result.Message, "x509:")
		return
	}

	pki := generateTestCerts(t, time.Now().Add(24*time.Hour))
	otherPKI := generateTestCerts(t, time.Now().Add(24*time.Hour))
	server := startTLSServer(t, pki, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, []byte(pki.caPEM), 0o600))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHTTPExecutor_Execute_MTLS_SystemRoots$", "-test.v")
	cmd.Env = append(os.Environ(),
		"SSL_CERT_FILE="+caFile,
		"PEEKAPING_TEST_MTLS_URL="+server.URL,
		"PEEKAPING_TEST_MTLS_CERT="+pki.clientCertPEM,
		"PEEKAPING_TEST_MTLS_KEY="+pki.clientKeyPEM,
		"PEEKAPING_TEST_MTLS_OTHER_CA="+otherPKI.caPEM,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	// A renamed test would match nothing and exit 0.
	require.Contains(t, string(out), "--- PASS: TestHTTPExecutor_Execute_MTLS_SystemRoots", string(out))
}

func TestHTTPExecutor_Execute_IgnoreTlsErrors(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create HTTPS test server with self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	tests := []struct {
		name            string
		ignoreTlsErrors bool
		expectedStatus  shared.MonitorStatus
		expectsError    bool
	}{
		{
			name:            "TLS errors ignored - should succeed",
			ignoreTlsErrors: true,
			expectedStatus:  shared.MonitorStatusUp,
			expectsError:    false,
		},
		{
			name:            "TLS errors not ignored - should fail",
			ignoreTlsErrors: false,
			expectedStatus:  shared.MonitorStatusDown,
			expectsError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monitor := &Monitor{
				ID:       "monitor1",
				Type:     "http",
				Name:     "Test Monitor",
				Interval: 30,
				Timeout:  5,
				Config: fmt.Sprintf(`{
					"url": "%s",
					"method": "GET",
					"encoding": "json",
					"accepted_statuscodes": ["2XX"],
					"authMethod": "none",
					"ignore_tls_errors": %t
				}`, server.URL, tt.ignoreTlsErrors),
			}

			result := executor.Execute(context.Background(), monitor, nil)
			assert.Equal(t, tt.expectedStatus, result.Status)

			if tt.expectsError {
				// Should contain some SSL/TLS error
				assert.Contains(t, strings.ToLower(result.Message), "tls")
			} else {
				// Should be successful
				assert.Contains(t, result.Message, "200")
			}
		})
	}
}

func TestCheckJsonQuery(t *testing.T) {
	responseBody := `{
  "userId": 1,
  "id": 1,
  "title": "delectus aut autem",
  "completed": false,
  "nested": {
    "value": "test"
  }
}`

	tests := []struct {
		name          string
		jsonQuery     string
		condition     string
		expectedValue string
		shouldSucceed bool
		shouldError   bool
	}{
		{
			name:          "gjson path - userId",
			jsonQuery:     "userId",
			condition:     "==",
			expectedValue: "1",
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "gjson path - title",
			jsonQuery:     "title",
			condition:     "==",
			expectedValue: "delectus aut autem",
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "empty query for full response (string comparison - backward compatibility)",
			jsonQuery:     "",
			condition:     "==",
			expectedValue: responseBody,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "empty query for full JSON equality - same structure",
			jsonQuery:     "",
			condition:     "==",
			expectedValue: `{"userId":1,"id":1,"title":"delectus aut autem","completed":false,"nested":{"value":"test"}}`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "empty query for full JSON equality - different key order",
			jsonQuery:     "",
			condition:     "==",
			expectedValue: `{"completed":false,"nested":{"value":"test"},"userId":1,"title":"delectus aut autem","id":1}`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "empty query for full JSON equality - different structure",
			jsonQuery:     "",
			condition:     "==",
			expectedValue: `{"userId":2,"id":1,"title":"different title","completed":true}`,
			shouldSucceed: false,
			shouldError:   false,
		},
		{
			name:          "empty query for full JSON inequality",
			jsonQuery:     "",
			condition:     "!=",
			expectedValue: `{"userId":2,"id":1,"title":"different title","completed":true}`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "empty query with invalid expected JSON",
			jsonQuery:     "",
			condition:     "==",
			expectedValue: `{"invalid": json}`,
			shouldSucceed: false,
			shouldError:   false,
		},

		{
			name:          "empty query for full JSON with unsupported condition",
			jsonQuery:     "",
			condition:     ">",
			expectedValue: `{"userId":1}`,
			shouldSucceed: false,
			shouldError:   true,
		},
		{
			name:          "non-existent path",
			jsonQuery:     "nonExistent",
			condition:     "==",
			expectedValue: "anything",
			shouldSucceed: false,
			shouldError:   true,
		},
		{
			name:          "nested gjson path",
			jsonQuery:     "nested.value",
			condition:     "==",
			expectedValue: "test",
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "boolean comparison",
			jsonQuery:     "completed",
			condition:     "==",
			expectedValue: "false",
			shouldSucceed: true,
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checkJsonQuery(responseBody, tt.jsonQuery, tt.condition, tt.expectedValue)

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.shouldSucceed, result)
			}
		})
	}
}

func TestCheckJsonQuery_Comparisons(t *testing.T) {
	responseBody := `{"count": 10, "name": "beta"}`

	tests := []struct {
		query     string
		condition string
		expected  string
		want      bool
	}{
		// Numbers compare as numbers: as strings, "10" sorts before "9".
		{"count", ">", "9", true},
		{"count", ">", "10", false},
		{"count", "<", "11", true},
		{"count", "<", "9", false},
		{"count", ">=", "10", true},
		{"count", ">=", "10.5", false},
		{"count", "<=", "10", true},
		{"count", "<=", "9.5", false},
		// Anything else compares as strings.
		{"name", ">", "alpha", true},
		{"name", "<", "alpha", false},
		{"name", ">=", "beta", true},
		{"name", "<=", "alpha", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %s %s", tt.query, tt.condition, tt.expected), func(t *testing.T) {
			got, err := checkJsonQuery(responseBody, tt.query, tt.condition, tt.expected)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCheckJsonQueryNonJsonResponse(t *testing.T) {
	nonJsonResponse := "This is not a JSON response"

	tests := []struct {
		name          string
		responseBody  string
		jsonQuery     string
		condition     string
		expectedValue string
		shouldSucceed bool
		shouldError   bool
	}{
		{
			name:          "empty query with non-JSON response body",
			responseBody:  nonJsonResponse,
			jsonQuery:     "",
			condition:     "==",
			expectedValue: `{"valid": "json"}`,
			shouldSucceed: false,
			shouldError:   false,
		},
		{
			name:          "empty query with non-JSON response - string comparison",
			responseBody:  nonJsonResponse,
			jsonQuery:     "",
			condition:     "==",
			expectedValue: nonJsonResponse,
			shouldSucceed: true,
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := checkJsonQuery(tt.responseBody, tt.jsonQuery, tt.condition, tt.expectedValue)

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.shouldSucceed, result)
			}
		})
	}
}

func TestCheckJsonValidation(t *testing.T) {
	tests := []struct {
		name          string
		responseBody  string
		expectedValue string
		shouldSucceed bool
		shouldError   bool
	}{
		{
			name:          "valid JSON comparison",
			responseBody:  `{"status": "ok", "code": 200}`,
			expectedValue: `{"status": "ok", "code": 200}`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "invalid JSON response body",
			responseBody:  `{"status": "ok", "code": 200`,
			expectedValue: `{"status": "ok", "code": 200}`,
			shouldSucceed: false,
			shouldError:   false,
		},
		{
			name:          "invalid JSON expected value",
			responseBody:  `{"status": "ok", "code": 200}`,
			expectedValue: `{"status": "ok", "code": 200`,
			shouldSucceed: false,
			shouldError:   false,
		},
		{
			name:          "null JSON comparison",
			responseBody:  `null`,
			expectedValue: `null`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "array JSON comparison",
			responseBody:  `[{"id": 1}, {"id": 2}]`,
			expectedValue: `[{"id": 1}, {"id": 2}]`,
			shouldSucceed: true,
			shouldError:   false,
		},
		{
			name:          "array JSON different order",
			responseBody:  `[{"id": 1}, {"id": 2}]`,
			expectedValue: `[{"id": 2}, {"id": 1}]`,
			shouldSucceed: false,
			shouldError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test with empty json_query to trigger full JSON comparison
			result, err := checkJsonQuery(tt.responseBody, "", "==", tt.expectedValue)

			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.shouldSucceed, result)
			}
		})
	}
}

func TestHTTPExecutor_Execute_IgnoreTlsErrors_WithMTLS(t *testing.T) {
	// Setup
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	// Create HTTPS test server with self-signed certificate
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: fmt.Sprintf(`{
			"url": "%s",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "mtls",
			"tlsCert": "invalid-cert",
			"tlsKey": "invalid-key",
			"tlsCa": "invalid-ca",
			"ignore_tls_errors": true
		}`, server.URL),
	}

	result := executor.Execute(context.Background(), monitor, nil)
	// Should fail because of invalid certificates, but test that the ignore_tls_errors flag is properly applied
	assert.Equal(t, shared.MonitorStatusDown, result.Status)
	assert.Contains(t, result.Message, "invalid mTLS cert/key")
}

func TestHTTPExecutor_Execute_DoesNotLeakConnections(t *testing.T) {
	logger := zap.NewNop().Sugar()
	executor := NewHTTPExecutor(logger)

	var mu sync.Mutex
	open := 0

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	// Count connections the server considers established (keep-alive idle
	// connections stay New-but-not-Closed, which is exactly the leak signal).
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch state {
		case http.StateNew:
			open++
		case http.StateClosed, http.StateHijacked:
			open--
		}
	}
	server.Start()
	defer server.Close()

	monitor := &Monitor{
		ID:       "monitor1",
		Type:     "http",
		Name:     "Leak Test Monitor",
		Interval: 30,
		Timeout:  5,
		Config: `{
			"url": "` + server.URL + `",
			"method": "GET",
			"encoding": "json",
			"accepted_statuscodes": ["2XX"],
			"authMethod": "none"
		}`,
	}

	const checks = 5
	for range checks {
		result := executor.Execute(context.Background(), monitor, nil)
		assert.Equal(t, shared.MonitorStatusUp, result.Status)
	}

	// After the checks complete, the server should observe its connections
	// close (asynchronously). If they leak, open stays at ~checks forever.
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return open <= 1
	}, 3*time.Second, 20*time.Millisecond, "established connections should not accumulate across checks")
}

// Every HTTP check shares one executor, and single mode runs up to
// CHECK_CONCURRENCY checks at once. Each check must use the client built
// from its own monitor: the TLS-skip monitor's client, not the strict one's,
// and the short-timeout monitor's client, not the long one's.
func TestHTTPExecutor_Execute_ConcurrentChecksKeepTheirOwnClient(t *testing.T) {
	executor := NewHTTPExecutor(zap.NewNop().Sugar())

	selfSigned := startTLSServer(t, generateTestCerts(t, time.Now().Add(24*time.Hour)), false,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(slow.Close)

	skipTLS := httpMonitor(t, "http", HTTPConfig{Url: selfSigned.URL, AuthMethod: "none", IgnoreTlsErrors: true})
	skipTLS.Name = "skip tls errors"
	strictTLS := httpMonitor(t, "http", HTTPConfig{Url: selfSigned.URL, AuthMethod: "none"})
	strictTLS.Name = "strict tls"
	shortTimeout := httpMonitor(t, "http", HTTPConfig{Url: slow.URL, AuthMethod: "none"})
	shortTimeout.Name = "short timeout"
	shortTimeout.Timeout = 1

	type check struct {
		monitor     *Monitor
		want        shared.MonitorStatus
		wantMessage string // a Down for another reason doesn't count
	}
	checks := []check{
		{skipTLS, shared.MonitorStatusUp, "200"},
		{strictTLS, shared.MonitorStatusDown, "certificate"},
		{shortTimeout, shared.MonitorStatusDown, "exceeded"}, // "context deadline exceeded" or "Client.Timeout exceeded"
	}

	const rounds = 20
	var wg sync.WaitGroup
	results := make([]*Result, rounds*len(checks))
	for i := 0; i < rounds; i++ {
		for j, c := range checks {
			wg.Add(1)
			go func(slot int, c check) {
				defer wg.Done()
				results[slot] = executor.Execute(context.Background(), c.monitor, nil)
			}(i*len(checks)+j, c)
		}
	}
	wg.Wait()

	for i := 0; i < rounds; i++ {
		for j, c := range checks {
			r := results[i*len(checks)+j]
			assert.Equalf(t, c.want, r.Status, "%s (round %d): %s", c.monitor.Name, i, r.Message)
			assert.Containsf(t, r.Message, c.wantMessage, "%s (round %d)", c.monitor.Name, i)
		}
	}
}

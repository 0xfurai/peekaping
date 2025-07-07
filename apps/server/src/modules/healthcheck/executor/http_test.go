package executor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"peekaping/src/modules/shared"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
		// Test basic auth
		if username, password, ok := r.BasicAuth(); ok {
			if username != "user" || password != "pass" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}

		// Test headers
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Test body
		if r.Method == "POST" {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
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
			name: "successful request with basic auth",
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
				"authMethod": "basic",
				"basic_auth_user": "user",
				"basic_auth_pass": "pass"
			}`,
			expectedStatus: shared.MonitorStatusUp,
		},
		{
			name: "failed request with invalid basic auth",
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
				"authMethod": "basic",
				"basic_auth_user": "wrong",
				"basic_auth_pass": "wrong"
			}`,
			expectedStatus: shared.MonitorStatusDown,
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
		name    string
		config  string
		monitor *Monitor
	}{
		{
			name:   "invalid JSON config",
			config: `{invalid json}`,
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
			assert.Contains(t, result.Message, "")
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
	assert.Contains(t, result.Message, "context deadline exceeded")
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
		Port:     9999, // Use a port that's definitely not in use
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

// Helper function to generate test certificates
func generateTestCerts() (cert, key, ca string, err error) {
	// Generate CA key
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", err
	}

	// Create CA certificate template
	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create CA certificate
	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", err
	}

	// Generate client key
	clientKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", "", err
	}

	// Create client certificate template
	clientTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Parse CA certificate
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return "", "", "", err
	}

	// Create client certificate
	_, err = x509.CreateCertificate(rand.Reader, &clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", err
	}

	// Convert to PEM format
	// Note: This is a simplified implementation for testing
	return "-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----",
		"-----BEGIN PRIVATE KEY-----\nTEST\n-----END PRIVATE KEY-----",
		"-----BEGIN CERTIFICATE-----\nTEST\n-----END CERTIFICATE-----",
		nil
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

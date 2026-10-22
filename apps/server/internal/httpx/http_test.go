package httpx_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestJSONAndText(t *testing.T) {
	w := httptest.NewRecorder()
	httpx.JSON(w, 201, map[string]string{"value": "<>&"})
	assert.Equal(t, 201, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, `{"value":"\u003c\u003e\u0026"}`, w.Body.String())
	w = httptest.NewRecorder()
	w.Header().Set("Content-Type", "image/svg+xml")
	httpx.Text(w, 200, `<svg width="100%">`)
	assert.Equal(t, `<svg width="100%">`, w.Body.String())
	assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
}

func TestCORSCompatibility(t *testing.T) {
	for _, tc := range []struct {
		method, origin string
		want           int
		cors           bool
	}{
		{"GET", "", 202, false},
		{"GET", "http://example.com", 202, false},
		{"GET", "https://client.example", 202, true},
		{"OPTIONS", "https://client.example", 204, true},
		{"OPTIONS", "", 202, false},
	} {
		r := httptest.NewRequest(tc.method, "http://example.com/api", nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		ran := false
		httpx.CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			w.WriteHeader(202)
		})).ServeHTTP(w, r)
		assert.Equal(t, tc.want, w.Code)
		assert.Equal(t, tc.want != 204, ran)
		if tc.cors {
			assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		}
		if tc.cors && tc.want != 204 {
			assert.Equal(t, "Authorization, X-Request-ID", w.Header().Get("Access-Control-Expose-Headers"))
		}
		if tc.want == 204 {
			assert.Equal(t, "GET,POST,PUT,DELETE,OPTIONS,PATCH", w.Header().Get("Access-Control-Allow-Methods"))
			assert.Equal(t, "Origin,X-Requested-With,Content-Type,Accept,Authorization,X-Request-ID", w.Header().Get("Access-Control-Allow-Headers"))
			assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
			assert.Empty(t, w.Header().Get("Vary"))
		}
	}
}

func TestRecoveryAndLogging(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	handler := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("broken") }), httpx.RequestID, httpx.Logger(logger, nil), httpx.Recovery(logger))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/panic", nil)
	r.Header.Set("X-Request-ID", "req-42")
	handler.ServeHTTP(w, r)
	assert.Equal(t, 500, w.Code)
	assert.Empty(t, w.Body.String())
	assert.Equal(t, "req-42", w.Header().Get("X-Request-ID"))
	assert.Equal(t, 1, logs.FilterMessage("HTTP handler panic").Len())
	assert.Equal(t, 1, logs.FilterMessage("HTTP request").Len())
	assert.Equal(t, "req-42", logs.FilterMessage("HTTP handler panic").All()[0].ContextMap()["request_id"])
	entry := logs.FilterMessage("HTTP request").All()[0].ContextMap()
	assert.Equal(t, "req-42", entry["request_id"])
	assert.EqualValues(t, 500, entry["status"])
	assert.Equal(t, "192.0.2.1", entry["client_ip"])
	assert.EqualValues(t, 0, entry["size"])
}

func TestAccessLogRecordsTheClientBehindATrustedProxy(t *testing.T) {
	for _, tc := range []struct{ name, trusted, want string }{
		{"forwarding headers are ignored from an untrusted address", "", "192.0.2.1"},
		{"forwarding headers count from a trusted proxy", "192.0.2.0/24", "198.51.100.7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proxies, err := httpx.ParseTrustedProxies(tc.trusted)
			require.NoError(t, err)
			core, logs := observer.New(zap.DebugLevel)
			handler := httpx.Logger(zap.New(core).Sugar(), proxies)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			r := httptest.NewRequest("GET", "/", nil)
			r.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")
			handler.ServeHTTP(httptest.NewRecorder(), r)
			assert.Equal(t, tc.want, logs.FilterMessage("HTTP request").All()[0].ContextMap()["client_ip"])
		})
	}
}

func TestLimitBody(t *testing.T) {
	handler := httpx.LimitBody(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			assert.EqualValues(t, 8, tooLarge.Limit)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		require.NoError(t, err)
		w.WriteHeader(http.StatusOK)
	}))
	for _, tc := range []struct {
		body string
		want int
	}{
		{"12345678", http.StatusOK},
		{"123456789", http.StatusRequestEntityTooLarge},
		{"", http.StatusOK},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader(tc.body)))
		assert.Equal(t, tc.want, w.Code, "%d bytes", len(tc.body))
	}
}

func TestAccessLogIncludesResponse(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	handler := httpx.Logger(zap.New(core).Sugar(), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Text(w, 418, "teapot")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/items?page=2", nil))
	entry := logs.FilterMessage("HTTP request").All()[0].ContextMap()
	assert.EqualValues(t, 418, entry["status"])
	assert.EqualValues(t, 6, entry["size"])
	assert.Equal(t, "GET", entry["method"])
	assert.NotContains(t, entry, "path")
}

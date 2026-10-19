package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestClientIPCompatibility(t *testing.T) {
	for _, tc := range []struct{ remote, forwarded, real, want string }{
		{"192.0.2.1:1234", "", "", "192.0.2.1"},
		{"[2001:db8::1]:1234", "", "", "2001:db8::1"},
		{"192.0.2.1:1234", "198.51.100.1, 192.0.2.2", "", "198.51.100.1"},
		{"192.0.2.1:1234", "invalid, 192.0.2.2", "198.51.100.2", "198.51.100.2"},
		{"192.0.2.1:1234", "invalid", "invalid", "192.0.2.1"},
		{"invalid", "198.51.100.1", "", ""},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		r.Header.Set("X-Real-IP", tc.real)
		assert.Equal(t, tc.want, httpx.ClientIP(r))
	}
}

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
		if tc.want == 204 {
			assert.Equal(t, "GET,POST,PUT,DELETE,OPTIONS,PATCH", w.Header().Get("Access-Control-Allow-Methods"))
			assert.Equal(t, "Origin,X-Requested-With,Content-Type,Accept,Authorization", w.Header().Get("Access-Control-Allow-Headers"))
			assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
			assert.Empty(t, w.Header().Get("Vary"))
		}
	}
}

func TestRecoveryAndLogging(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core).Sugar()
	handler := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("broken") }), httpx.Logger(logger), httpx.Recovery(logger))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/panic", nil))
	assert.Equal(t, 500, w.Code)
	assert.Empty(t, w.Body.String())
	assert.Equal(t, 1, logs.FilterMessage("HTTP handler panic").Len())
	assert.Equal(t, 1, logs.FilterMessage("HTTP request").Len())
}

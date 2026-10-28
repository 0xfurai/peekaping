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

// corsOrigins is the allowed set for the CORS_ORIGINS list, with https://peek.example as CLIENT_URL.
func corsOrigins(t *testing.T, list string) *httpx.CORSOrigins {
	t.Helper()
	origins, err := httpx.ParseCORSOrigins(list, "https://peek.example/")
	require.NoError(t, err)
	return origins
}

// serveCORS sends a request for http://example.com/api from origin (none when empty) through
// httpx.CORS, to a handler that answers 202, and reports whether the handler ran.
func serveCORS(origins *httpx.CORSOrigins, method, origin string) (w *httptest.ResponseRecorder, ran bool) {
	r := httptest.NewRequest(method, "http://example.com/api", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if method == http.MethodOptions {
		r.Header.Set("Access-Control-Request-Method", "POST")
	}
	w = httptest.NewRecorder()
	httpx.CORS(origins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ran = true
		w.WriteHeader(202)
	})).ServeHTTP(w, r)
	return w, ran
}

func accessControlHeaders(w *httptest.ResponseRecorder) []string {
	var names []string
	for name := range w.Header() {
		if strings.HasPrefix(name, "Access-Control-") {
			names = append(names, name)
		}
	}
	return names
}

func TestCORS_AnAllowedOriginReadsTheResponse(t *testing.T) {
	w, ran := serveCORS(corsOrigins(t, "https://client.example,https://other.example"), "GET", "https://client.example")

	assert.True(t, ran)
	assert.Equal(t, 202, w.Code)
	assert.Equal(t, "https://client.example", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Authorization, X-Request-ID", w.Header().Get("Access-Control-Expose-Headers"))
	assert.Equal(t, []string{"Origin"}, w.Header().Values("Vary"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_APreflightFromAnAllowedOriginIsAnswered(t *testing.T) {
	w, ran := serveCORS(corsOrigins(t, "https://client.example"), "OPTIONS", "https://client.example")

	assert.False(t, ran)
	assert.Equal(t, 204, w.Code)
	assert.Equal(t, "https://client.example", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET,POST,PUT,DELETE,OPTIONS,PATCH", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Origin,X-Requested-With,Content-Type,Accept,Authorization,X-Request-ID,X-Workspace-ID", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, []string{"Origin"}, w.Header().Values("Vary"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, w.Header().Get("Access-Control-Max-Age"))
}

func TestCORS_AnOriginNotInTheListGetsNoCORSHeaders(t *testing.T) {
	origins := corsOrigins(t, "https://client.example")
	for _, origin := range []string{
		"https://evil.example",
		"http://client.example",       // another scheme
		"https://client.example:8443", // another port
		"https://sub.client.example",
		"null",
	} {
		for _, method := range []string{"GET", "POST", "OPTIONS"} {
			w, ran := serveCORS(origins, method, origin)

			// The request is served as before; the browser just can't read the answer.
			assert.True(t, ran, "%s from %s", method, origin)
			assert.Equal(t, 202, w.Code, "%s from %s", method, origin)
			assert.Empty(t, accessControlHeaders(w), "%s from %s", method, origin)
		}
	}
}

func TestCORS_ASameOriginRequestIsLeftAlone(t *testing.T) {
	origins := corsOrigins(t, "https://client.example")
	for _, tc := range []struct{ method, origin string }{
		{"GET", ""},
		{"OPTIONS", ""},
		{"GET", "http://example.com"},
		{"GET", "https://example.com"},
		{"OPTIONS", "http://example.com"},
	} {
		w, ran := serveCORS(origins, tc.method, tc.origin)

		assert.True(t, ran, "%s from %q", tc.method, tc.origin)
		assert.Equal(t, 202, w.Code, "%s from %q", tc.method, tc.origin)
		assert.Empty(t, accessControlHeaders(w), "%s from %q", tc.method, tc.origin)
	}
}

// With a list, the answer depends on the request's Origin, so a cache must keep a copy per Origin.
// Otherwise a copy that has no CORS headers, made for a request without an Origin or from a
// disallowed one, could be served to an allowed origin.
func TestCORS_AListVariesEveryResponseByOrigin(t *testing.T) {
	origins := corsOrigins(t, "https://client.example")
	for _, tc := range []struct{ name, method, origin string }{
		{"no Origin", "GET", ""},
		{"no Origin, preflight", "OPTIONS", ""},
		{"the same origin", "GET", "https://example.com"},
		{"a disallowed origin", "GET", "https://evil.example"},
		{"a disallowed origin, preflight", "OPTIONS", "https://evil.example"},
		{"an allowed origin", "GET", "https://client.example"},
		{"an allowed origin, preflight", "OPTIONS", "https://client.example"},
	} {
		w, _ := serveCORS(origins, tc.method, tc.origin)

		assert.Equal(t, []string{"Origin"}, w.Header().Values("Vary"), tc.name)
	}
}

func TestCORS_AStarAllowsAnyOriginWithoutCredentials(t *testing.T) {
	origins := corsOrigins(t, "*")

	w, ran := serveCORS(origins, "GET", "https://anything.example")
	assert.True(t, ran)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Authorization, X-Request-ID", w.Header().Get("Access-Control-Expose-Headers"))
	assert.Empty(t, w.Header().Values("Vary"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))

	w, ran = serveCORS(origins, "OPTIONS", "https://anything.example")
	assert.False(t, ran)
	assert.Equal(t, 204, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET,POST,PUT,DELETE,OPTIONS,PATCH", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_AnUnsetListAllowsTheOriginOfClientURL(t *testing.T) {
	origins := corsOrigins(t, "")

	w, _ := serveCORS(origins, "GET", "https://peek.example")
	assert.Equal(t, "https://peek.example", w.Header().Get("Access-Control-Allow-Origin"))

	w, ran := serveCORS(origins, "GET", "https://evil.example")
	assert.True(t, ran)
	assert.Empty(t, accessControlHeaders(w))
}

func TestCORS_WithoutOriginsNoOriginIsAllowed(t *testing.T) {
	for _, origins := range []*httpx.CORSOrigins{nil, {}} {
		w, ran := serveCORS(origins, "GET", "https://client.example")

		assert.True(t, ran)
		assert.Empty(t, accessControlHeaders(w))
		assert.Empty(t, w.Header().Values("Vary"))
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
		// A chunked body: no Content-Length is declared, so the limit stops the read instead
		r := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		r.ContentLength = -1
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		assert.Equal(t, tc.want, w.Code, "%d bytes", len(tc.body))
	}
}

func TestLimitBody_RefusesADeclaredLengthOverTheLimitWithoutRunningTheHandler(t *testing.T) {
	ran := false
	handler := httpx.LimitBody(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ran = true }))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader("123456789")))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.JSONEq(t, `{"message":"request body too large","data":null}`, w.Body.String())
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.False(t, ran)

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/", strings.NewReader("12345678")))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ran, "a declared length at the limit goes through")
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

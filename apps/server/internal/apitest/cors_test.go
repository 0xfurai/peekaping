package apitest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fromOrigin sets the Origin header, as a browser does for a request from a page on origin.
func fromOrigin(origin string) credentials {
	return func(req *http.Request) { req.Header.Set("Origin", origin) }
}

func asPreflight(req *http.Request) { req.Header.Set("Access-Control-Request-Method", "POST") }

func accessControlHeaders(w *httptest.ResponseRecorder) []string {
	var names []string
	for name := range w.Header() {
		if strings.HasPrefix(name, "Access-Control-") {
			names = append(names, name)
		}
	}
	return names
}

func TestCORS_TheOriginOfClientURLCanReadTheAPI(t *testing.T) {
	a := newAppWithAdmin(t)
	const webApp = "https://peek.example" // clientURL without its path

	w := a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin, fromOrigin(webApp))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, webApp, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, []string{"Origin"}, w.Header().Values("Vary"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))

	w = a.request(t, http.MethodOptions, "/api/v1/auth/login", nil, fromOrigin(webApp), asPreflight)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Equal(t, webApp, w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET,POST,PUT,DELETE,OPTIONS,PATCH", w.Header().Get("Access-Control-Allow-Methods"))
	assert.Equal(t, "Origin,X-Requested-With,Content-Type,Accept,Authorization,X-Request-ID,X-Workspace-ID", w.Header().Get("Access-Control-Allow-Headers"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_APageOnAnotherOriginIsServedButCannotReadTheAnswer(t *testing.T) {
	a := newAppWithAdmin(t)
	const evil = "http://evil.example"

	// The request is served as it would be without CORS: the page's browser just won't hand
	// the response over.
	w := a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin, fromOrigin(evil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, accessControlHeaders(w))

	w = a.request(t, http.MethodGet, "/api/v1/monitors", nil, fromOrigin(evil))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Empty(t, accessControlHeaders(w))

	w = a.request(t, http.MethodOptions, "/api/v1/auth/login", nil, fromOrigin(evil), asPreflight)
	assert.Empty(t, accessControlHeaders(w))
}

func TestCORS_ListedOriginsReplaceTheOriginOfClientURL(t *testing.T) {
	a := newApp(t, withCORSOrigins("http://localhost:8383, https://other.example"))

	for _, origin := range []string{"http://localhost:8383", "https://other.example"} {
		w := a.request(t, http.MethodGet, "/api/v1/health", nil, fromOrigin(origin))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, origin, w.Header().Get("Access-Control-Allow-Origin"), origin)
	}

	w := a.request(t, http.MethodGet, "/api/v1/health", nil, fromOrigin("https://peek.example"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, accessControlHeaders(w))
}

func TestCORS_AStarAllowsAnyOriginWithoutCredentials(t *testing.T) {
	a := newApp(t, withCORSOrigins("*"))

	w := a.request(t, http.MethodGet, "/api/v1/health", nil, fromOrigin("http://evil.example"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))

	w = a.request(t, http.MethodOptions, "/api/v1/auth/login", nil, fromOrigin("http://evil.example"), asPreflight)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
}

func TestCORS_SameOriginRequestsAreUnaffected(t *testing.T) {
	a := newApp(t)
	// httptest.NewRequest addresses the request to example.com, so that is the host it came from.
	for _, origin := range []string{"", "http://example.com", "https://example.com"} {
		w := a.request(t, http.MethodGet, "/api/v1/health", nil, func(req *http.Request) {
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Empty(t, accessControlHeaders(w), origin)
	}
}

func TestCORS_SocketIOPollingAnswersTheAllowedOriginOnce(t *testing.T) {
	a := newApp(t)
	const path = "/socket.io/?EIO=4&transport=polling"

	// engine.io sets no CORS headers of its own, so the router's are the only ones
	w := a.request(t, http.MethodGet, path, nil, fromOrigin("https://peek.example"))
	assert.Equal(t, []string{"https://peek.example"}, w.Header().Values("Access-Control-Allow-Origin"))

	w = a.request(t, http.MethodGet, path, nil, fromOrigin("http://evil.example"))
	assert.Empty(t, accessControlHeaders(w))
}

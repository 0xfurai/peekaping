package apitest

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// publicRoutes answer without credentials. Every other route must return 401.
var publicRoutes = map[string]string{
	"GET /openapi.json":           "OpenAPI spec",
	"GET /openapi.yaml":           "OpenAPI spec",
	"GET /openapi-3.0.json":       "OpenAPI 3.0 spec",
	"GET /openapi-3.0.yaml":       "OpenAPI 3.0 spec",
	"GET /docs":                   "API reference",
	"GET /health":                 "server liveness",
	"GET /metrics":                "Prometheus scrape target, served only when METRICS_ENABLED is set",
	"GET /api/v1/health":          "server liveness",
	"GET /api/v1/version":         "server version",
	"GET /swagger/{any...}":       "API docs",
	"GET /socket.io/{f...}":       "socket.io checks the access token when the socket connects",
	"POST /socket.io/{f...}":      "socket.io checks the access token when the socket connects",
	"POST /api/v1/auth/register":  "creates the first admin and refuses once one exists",
	"POST /api/v1/auth/login":     "login",
	"POST /api/v1/auth/refresh":   "authenticated by the refresh token in the body",
	"GET /api/v1/api-keys/config": "the API key prefix",

	"GET /api/v1/status-pages/slug/{slug}":                   "published status page",
	"GET /api/v1/status-pages/domain/{domain}":               "published status page on a custom domain",
	"GET /api/v1/status-pages/slug/{slug}/monitors":          "monitors on a published status page",
	"GET /api/v1/status-pages/slug/{slug}/monitors/homepage": "monitors on a published status page",

	"GET /api/v1/badge/{monitorId}/status":            "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/uptime":            "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/uptime/{duration}": "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/ping":              "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/ping/{duration}":   "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/cert-exp":          "badge for a monitor on a published status page",
	"GET /api/v1/badge/{monitorId}/response":          "badge for a monitor on a published status page",

	"GET /api/v1/push/{token}": "push URL, authenticated by its push token",
}

// jwtOnlyRoutes manage the account, its API keys, and install-wide choices that are the
// admin's alone, so an API key must not reach them.
var jwtOnlyRoutes = map[string]bool{
	"POST /api/v1/auth/2fa/setup":   true,
	"POST /api/v1/auth/2fa/verify":  true,
	"POST /api/v1/auth/2fa/disable": true,
	"PUT /api/v1/auth/password":     true,
	"POST /api/v1/api-keys":         true,
	"GET /api/v1/api-keys":          true,
	"GET /api/v1/api-keys/{id}":     true,
	"PUT /api/v1/api-keys/{id}":     true,
	"DELETE /api/v1/api-keys/{id}":  true,
	"PUT /api/v1/telemetry":         true,
	"POST /api/v1/telemetry/notice": true,
	"PUT /api/v1/error-reporting":   true,
}

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

type route struct{ method, path string }

func (r route) key() string { return r.method + " " + r.path }

// url fills every path parameter with a value no record has.
func (r route) url() string { return pathParam.ReplaceAllString(r.path, "missing") }

func (a *app) routes() []route {
	var routes []route
	for _, info := range a.router.Routes() {
		routes = append(routes, route{info.Method, info.Path})
	}
	return routes
}

func TestRouteTable_AllowlistsOnlyRegisteredRoutes(t *testing.T) {
	registered := map[string]bool{}
	for _, r := range newApp(t, withMetrics).routes() {
		registered[r.key()] = true
	}
	for key := range publicRoutes {
		assert.True(t, registered[key], "public route %q is not registered", key)
	}
	for key := range jwtOnlyRoutes {
		assert.True(t, registered[key], "JWT-only route %q is not registered", key)
		assert.NotContains(t, publicRoutes, key)
	}
}

func TestRouteTable_ProtectedRoutesRequireCredentials(t *testing.T) {
	a := newAppWithAdmin(t, withMetrics)
	invalid := map[string]credentials{
		"no credentials": func(*http.Request) {},
		"invalid JWT":    func(req *http.Request) { req.Header.Set("Authorization", "Bearer not-a-token") },
		"invalid API key": func(req *http.Request) {
			req.Header.Set("X-API-Key", "pk_not-a-key")
		},
	}
	routes := a.routes()
	require.NotEmpty(t, routes)
	for _, r := range routes {
		if _, public := publicRoutes[r.key()]; public {
			continue
		}
		for name, creds := range invalid {
			t.Run(r.key()+"/"+name, func(t *testing.T) {
				w := a.request(t, r.method, r.url(), nil, creds)
				assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			})
		}
	}
}

func TestRouteTable_APIKeysReachOnlySharedRoutes(t *testing.T) {
	a := newAppWithAdmin(t, withMetrics)
	for _, r := range a.routes() {
		if _, public := publicRoutes[r.key()]; public {
			continue
		}
		t.Run(r.key(), func(t *testing.T) {
			w := a.request(t, r.method, r.url(), nil, a.withAPIKey)
			if jwtOnlyRoutes[r.key()] {
				assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
			} else {
				assert.NotEqual(t, http.StatusUnauthorized, w.Code, w.Body.String())
			}
		})
	}
}

func TestRouteTable_SettingsKeepSigningSecretsFromAPIKeys(t *testing.T) {
	a := newAppWithAdmin(t)
	for _, key := range []string{"ACCESS_TOKEN_SECRET_KEY", "REFRESH_TOKEN_SECRET_KEY", "auth.access_token_secret", "auth.refresh_token_secret"} {
		for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			t.Run(method+"/"+key, func(t *testing.T) {
				w := a.request(t, method, "/api/v1/settings/key/"+key, map[string]string{"value": "attacker-chosen", "type": "string"}, a.withAPIKey)
				assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			})
		}
	}
	// The admin's session survives, so the secrets were neither read nor changed.
	w := a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

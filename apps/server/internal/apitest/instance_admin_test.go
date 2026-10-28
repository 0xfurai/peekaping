package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
)

// installRoutes are the routes that change what belongs to the install and not to a workspace:
// only an instance admin may call them (docs/adr/0005 section 10)
var installRoutes = []call{
	{"turn the usage ping off", http.MethodPut, "/api/v1/telemetry", map[string]any{"enabled": false}},
	{"dismiss the notice", http.MethodPost, "/api/v1/telemetry/notice", nil},
	{"turn error reports off", http.MethodPut, "/api/v1/error-reporting", map[string]any{"enabled": false}},
}

func TestInstanceAdmin_AUserWithoutTheFlagIsForbiddenAnInstallRoute(t *testing.T) {
	a := newAppWithAdmin(t)
	// Even an owner of a workspace manages only that workspace
	owner := a.userIn(t, "owner@example.com", a.firstWorkspace(t), workspace.RoleOwner)

	for _, route := range installRoutes {
		t.Run(route.name, func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, owner)
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
	}
	// and nothing changed
	state := telemetryBody(t, a.request(t, http.MethodGet, telemetryPath, nil, a.asAdmin), http.StatusOK)
	assert.True(t, state.Enabled)
}

func TestInstanceAdmin_AnInstanceAdminMayChangeTheInstall(t *testing.T) {
	a := newAppWithAdmin(t)
	// A user in no workspace at all can be an instance admin: the flag gives no access to a workspace
	userID := a.createUser(t, "ops@example.com")
	a.setInstanceAdmin(t, userID, true)
	ops := a.loginAs(t, "ops@example.com")

	for _, route := range installRoutes {
		t.Run(route.name, func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, ops)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, "/api/v1/monitors", nil, ops).Code, "and still sees no workspace")
}

// The flag is read on every request: a token issued to an instance admin stops working for the
// install routes when the flag is taken away, and works again when it is given back
func TestInstanceAdmin_TheFlagIsReadOnEveryRequest(t *testing.T) {
	a := newAppWithAdmin(t)
	adminID := a.userID(t, adminEmail)
	change := installRoutes[0]

	assert.Equal(t, http.StatusOK, a.request(t, change.method, change.path, change.body, a.asAdmin).Code)
	a.setInstanceAdmin(t, adminID, false)
	assert.Equal(t, http.StatusForbidden, a.request(t, change.method, change.path, change.body, a.asAdmin).Code)
	a.setInstanceAdmin(t, adminID, true)
	assert.Equal(t, http.StatusOK, a.request(t, change.method, change.path, change.body, a.asAdmin).Code)
}

// An API key belongs to a workspace and is never an instance admin
func TestInstanceAdmin_AnAPIKeyCanNotChangeTheInstall(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, route := range installRoutes {
		t.Run(route.name, func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, a.withAPIKey)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "the route takes the JWT of a signed-in user only: "+w.Body.String())
		})
	}
}

package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyNames lists the names of the API keys the request sees, which are the keys of the workspace
// it runs in. API keys are the first records a workspace owns.
func (a *app) keyNames(t *testing.T, creds ...credentials) []string {
	t.Helper()
	keys := decode[[]apiKeyResponse](t, a.request(t, http.MethodGet, "/api/v1/api-keys", nil, creds...), http.StatusOK)
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = key.Name
	}
	return names
}

func (a *app) createKey(t *testing.T, name string, creds ...credentials) string {
	t.Helper()
	return decode[apiKeyResponse](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": name}, creds...), http.StatusCreated).ID
}

// workspaceRoutes are one read of each route group whose records belong to a workspace
var workspaceRoutes = []call{
	{"monitors", http.MethodGet, "/api/v1/monitors", nil},
	{"notification channels", http.MethodGet, "/api/v1/notification-channels", nil},
	{"status pages", http.MethodGet, "/api/v1/status-pages", nil},
	{"proxies", http.MethodGet, "/api/v1/proxies", nil},
	{"maintenances", http.MethodGet, "/api/v1/maintenances", nil},
	{"tags", http.MethodGet, "/api/v1/tags", nil},
	{"settings", http.MethodGet, "/api/v1/settings/key/KEEP_DATA_PERIOD_DAYS", nil},
	{"API keys", http.MethodGet, "/api/v1/api-keys", nil},
}

func TestWorkspaceRequests_AMemberRunsInTheWorkspaceTheHeaderNames(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	a.addMember(t, second, a.userID(t, adminEmail), workspace.RoleAdmin)
	a.createKey(t, "second's key", a.asAdmin, inWorkspace(second))

	assert.Equal(t, []string{"second's key"}, a.keyNames(t, a.asAdmin, inWorkspace(second)))
	assert.Equal(t, []string{"apitest"}, a.keyNames(t, a.asAdmin, inWorkspace(a.firstWorkspace(t))))
}

func TestWorkspaceRequests_WithoutTheHeaderARequestRunsInTheDefaultWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)
	// The admin joined the first workspace first, so it stays their default whatever joins after
	for _, name := range []string{"Aardvark", "Second"} {
		a.addMember(t, a.createWorkspace(t, name), a.userID(t, adminEmail), workspace.RoleOwner)
	}

	assert.Equal(t, []string{"apitest"}, a.keyNames(t, a.asAdmin))

	// and for a user whose first workspace is not the install's first
	later := a.createWorkspace(t, "Later")
	other := a.createWorkspace(t, "Other")
	bobID := a.createUser(t, "bob@example.com")
	a.addMember(t, other, bobID, workspace.RoleAdmin)
	a.addMember(t, later, bobID, workspace.RoleAdmin)
	bob := a.loginAs(t, "bob@example.com")
	a.createKey(t, "other's key", bob, inWorkspace(other))
	a.createKey(t, "later's key", bob, inWorkspace(later))
	assert.Equal(t, []string{"other's key"}, a.keyNames(t, bob), "the workspace Bob joined first")
}

func TestWorkspaceRequests_AWorkspaceTheUserIsNotInAnswers404(t *testing.T) {
	a := newAppWithAdmin(t)
	elsewhere := a.createWorkspace(t, "Elsewhere")
	bob := a.userIn(t, "bob@example.com", elsewhere, workspace.RoleOwner)

	for name, header := range map[string]string{
		"a workspace of someone else": string(a.firstWorkspace(t)),
		"an ID that doesn't exist":    uuid.NewString(),
		"not a UUID":                  "not-a-uuid",
		"an empty-looking ID":         "00000000-0000-0000-0000-000000000000",
		"a SQL injection":             "' OR 1=1 --",
	} {
		for _, route := range workspaceRoutes {
			t.Run(name+"/"+route.name, func(t *testing.T) {
				w := a.request(t, route.method, route.path, route.body, bob, withHeader("X-Workspace-ID", header))
				assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
				assert.JSONEq(t, `{"message":"Workspace not found","data":null}`, w.Body.String())
			})
		}
	}
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bob, inWorkspace(elsewhere)).Code)
}

// A key made in a workspace runs there, whatever workspaces its maker is in
func TestAPIKeyWorkspace_AKeyRunsInTheWorkspaceItWasCreatedIn(t *testing.T) {
	a := newAppWithAdmin(t)
	second := a.createWorkspace(t, "Second")
	a.addMember(t, second, a.userID(t, adminEmail), workspace.RoleOwner)
	token := decode[struct {
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "second's"}, a.asAdmin, inWorkspace(second)), http.StatusCreated).Token
	secondKey := withHeader("X-API-Key", token)

	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, secondKey).Code)
	// and it may name its own workspace
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, secondKey, inWorkspace(second)).Code)
}

func TestAPIKeyWorkspace_AKeyThatNamesAnotherWorkspaceAnswers404(t *testing.T) {
	a := newAppWithAdmin(t)
	other := a.createWorkspace(t, "Other")
	for name, header := range map[string]string{
		"another workspace": string(other),
		"an unknown ID":     uuid.NewString(),
		"not a UUID":        "nope",
	} {
		// The API key routes take no API key, so they answer 401 before any workspace
		for _, route := range workspaceRoutes[:len(workspaceRoutes)-1] {
			t.Run(name+"/"+route.name, func(t *testing.T) {
				w := a.request(t, route.method, route.path, route.body, a.withAPIKey, withHeader("X-Workspace-ID", header))
				assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			})
		}
	}
}

func TestAPIKeyWorkspace_KeysAreManagedOnlyWithinTheirWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)
	other := a.createWorkspace(t, "Other")
	a.addMember(t, other, a.userID(t, adminEmail), workspace.RoleAdmin)
	foreign := a.createKey(t, "foreign", a.asAdmin, inWorkspace(other))
	path := "/api/v1/api-keys/" + foreign

	// From the first workspace, the other's key is not there to list, read, change or revoke
	assert.Equal(t, []string{"apitest"}, a.keyNames(t, a.asAdmin))
	for _, c := range []call{
		{"read", http.MethodGet, path, nil},
		{"change", http.MethodPut, path, map[string]any{"name": "mine now"}},
		{"revoke", http.MethodDelete, path, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := a.request(t, c.method, c.path, c.body, a.asAdmin)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		})
	}
	// and by an API key of the first workspace
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)

	// The key is untouched, and its own workspace still has it
	assert.Equal(t, []string{"foreign"}, a.keyNames(t, a.asAdmin, inWorkspace(other)))
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, path, nil, a.asAdmin, inWorkspace(other)).Code)
}

func TestAPIKeyWorkspace_OnlyAnAdminOrOwnerManagesKeys(t *testing.T) {
	a := newAppWithAdmin(t)
	first := a.firstWorkspace(t)
	member := a.userIn(t, "member@example.com", first, workspace.RoleMember)
	admin := a.userIn(t, "admin2@example.com", first, workspace.RoleAdmin)
	key := a.createKey(t, "kept", a.asAdmin)
	path := "/api/v1/api-keys/" + key

	for _, c := range []call{
		{"create", http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "new"}},
		{"list", http.MethodGet, "/api/v1/api-keys", nil},
		{"read", http.MethodGet, path, nil},
		{"change", http.MethodPut, path, map[string]any{"name": "renamed"}},
		{"revoke", http.MethodDelete, path, nil},
	} {
		t.Run("a member can't "+c.name, func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, a.request(t, c.method, c.path, c.body, member).Code)
		})
	}
	assert.Equal(t, []string{"kept", "apitest"}, a.keyNames(t, admin), "an admin may, and nothing changed")
	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, path, nil, admin).Code)
	// but a member still works with everything else in the workspace
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, member).Code)
}

func TestAPIKeyWorkspace_ANewKeyNeverTakesItsWorkspaceFromTheBody(t *testing.T) {
	a := newAppWithAdmin(t)
	other := a.createWorkspace(t, "Other")

	a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "sneaky", "workspace_id": string(other), "workspaceId": string(other)}, a.asAdmin)

	assert.Equal(t, []string{"sneaky", "apitest"}, a.keyNames(t, a.asAdmin))
}

// A user who was removed from every workspace can still sign in and manage their account
func TestWorkspaceRequests_AUserInNoWorkspaceCanManageTheirAccountAndReachNoWorkspaceRoute(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createUser(t, "loner@example.com")
	newPassword := "Another-horse-2"

	session := decode[tokens](t, a.login(t, map[string]any{"email": "loner@example.com", "password": adminPassword}), http.StatusOK)
	loner := bearer(session.AccessToken)

	refreshed := decode[tokens](t, a.refresh(t, session.RefreshToken), http.StatusOK)
	assert.NotEmpty(t, refreshed.AccessToken)
	setup := a.request(t, http.MethodPost, "/api/v1/auth/2fa/setup", map[string]any{"email": "loner@example.com", "password": adminPassword}, loner)
	assert.Equal(t, http.StatusOK, setup.Code, setup.Body.String())
	changed := a.request(t, http.MethodPut, "/api/v1/auth/password", map[string]any{"currentPassword": adminPassword, "newPassword": newPassword}, loner)
	assert.Equal(t, http.StatusOK, changed.Code, changed.Body.String())

	for _, route := range workspaceRoutes {
		t.Run(route.name, func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, loner)
			assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
			assert.JSONEq(t, `{"message":"Workspace not found","data":null}`, w.Body.String())
		})
	}
}

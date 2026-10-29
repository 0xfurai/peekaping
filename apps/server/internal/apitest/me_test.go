package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type meResponse struct {
	User struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		InstanceAdmin bool   `json:"instanceAdmin"`
	} `json:"user"`
	Workspaces []struct {
		ID              string  `json:"id"`
		Name            string  `json:"name"`
		Role            string  `json:"role"`
		Suspended       bool    `json:"suspended"`
		SuspendedReason *string `json:"suspendedReason"`
	} `json:"workspaces"`
	DefaultWorkspaceID   *string  `json:"defaultWorkspaceId"`
	DisabledMonitorTypes []string `json:"disabledMonitorTypes"`
	MinCheckInterval     int      `json:"minCheckInterval"`
}

func TestMe_ReturnsTheUserTheirWorkspaceAndTheDefault(t *testing.T) {
	a := newAppWithAdmin(t)

	me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, a.userID(t, adminEmail), me.User.ID)
	assert.Equal(t, adminEmail, me.User.Email)
	assert.True(t, me.User.InstanceAdmin, "the first user manages the install")
	require.Len(t, me.Workspaces, 1)
	assert.Equal(t, string(a.firstWorkspace(t)), me.Workspaces[0].ID)
	assert.Equal(t, "My workspace", me.Workspaces[0].Name)
	assert.Equal(t, "owner", me.Workspaces[0].Role)
	require.NotNil(t, me.DefaultWorkspaceID)
	assert.Equal(t, me.Workspaces[0].ID, *me.DefaultWorkspaceID)
}

// The answer names the user and their workspaces, so a browser must not keep it on disk
func TestMe_IsNotStoredByTheBrowser(t *testing.T) {
	a := newAppWithAdmin(t)

	w := a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestMe_ListsEveryWorkspaceWithTheRoleInEachAndTheOneJoinedFirstAsDefault(t *testing.T) {
	a := newAppWithAdmin(t)
	// Bob joins Beta before Alpha: the order is the order of joining, not of names
	beta, alpha := a.createWorkspace(t, "Beta"), a.createWorkspace(t, "Alpha")
	bobID := a.createUser(t, "bob@example.com")
	a.addMember(t, beta, bobID, workspace.RoleMember)
	a.addMember(t, alpha, bobID, workspace.RoleAdmin)

	me := decode[meResponse](t, a.request(t, http.MethodGet, "/api/v1/me", nil, a.loginAs(t, "bob@example.com")), http.StatusOK)

	assert.False(t, me.User.InstanceAdmin)
	require.Len(t, me.Workspaces, 2)
	assert.Equal(t, "Beta", me.Workspaces[0].Name)
	assert.Equal(t, "member", me.Workspaces[0].Role)
	assert.Equal(t, "Alpha", me.Workspaces[1].Name)
	assert.Equal(t, "admin", me.Workspaces[1].Role)
	require.NotNil(t, me.DefaultWorkspaceID)
	assert.Equal(t, string(beta), *me.DefaultWorkspaceID)
}

func TestMe_AUserInNoWorkspaceHasNoDefault(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createUser(t, "loner@example.com")

	w := a.request(t, http.MethodGet, "/api/v1/me", nil, a.loginAs(t, "loner@example.com"))

	me := decode[meResponse](t, w, http.StatusOK)
	assert.Equal(t, "loner@example.com", me.User.Email)
	assert.NotNil(t, me.Workspaces, "an empty list, not null")
	assert.Empty(t, me.Workspaces)
	assert.Nil(t, me.DefaultWorkspaceID)
	assert.Contains(t, w.Body.String(), `"defaultWorkspaceId":null`)
	assert.Contains(t, w.Body.String(), `"workspaces":[]`)
}

func TestMe_IsForASignedInUserNotAnAPIKey(t *testing.T) {
	a := newAppWithAdmin(t)

	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, a.withAPIKey).Code)
}

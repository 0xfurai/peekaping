package apitest

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/workspace"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-170: users delete their own account after entering their password. They are refused while
// they are the only owner of a workspace that is not scheduled for deletion, and when they are the
// last instance admin, as an instance admin deleting them is.

const accountPath = "/api/v1/auth/me"

// deleteAccount asks to delete the signed-in user's own account with the password
func (a *app) deleteAccount(t *testing.T, password string, creds ...credentials) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodDelete, accountPath, map[string]any{"password": password}, creds...)
}

// lastOwnerRefusal is the 409 for the last owner of workspaces
type lastOwnerRefusal struct {
	Error struct {
		Code       string `json:"code"`
		Workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workspaces"`
	} `json:"error"`
}

// soleOwner is a user who is the only owner of a workspace that has another member
func (a *app) soleOwner(t *testing.T) (email string, id workspace.ID, creds credentials) {
	t.Helper()
	email = "dana@example.com"
	id = a.createWorkspace(t, "Dana's team")
	creds = a.userIn(t, email, id, workspace.RoleOwner)
	a.addMember(t, id, a.createUser(t, "erin@example.com"), workspace.RoleMember)
	return email, id, creds
}

func TestAccountDeletion_APasswordThatIsWrongDeletesNothing(t *testing.T) {
	a := newAppWithAdmin(t)
	creds := a.userIn(t, "dana@example.com", a.firstWorkspace(t), workspace.RoleMember)

	for _, password := range []string{"wrong password", ""} {
		res := a.deleteAccount(t, password, creds)
		assert.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	}
	assert.Contains(t, a.deleteAccount(t, "wrong password", creds).Body.String(), "current password is incorrect", "as the password change says it")

	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, creds).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodDelete, accountPath, map[string]any{"password": adminPassword}).Code, "no sign-in")
	assert.Equal(t, http.StatusUnauthorized, a.deleteAccount(t, adminPassword, a.withAPIKey).Code, "an API key has no account")
}

func TestAccountDeletion_TheOnlyOwnerOfAWorkspaceIsRefusedAndTheWorkspaceIsNamed(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		_, sole, creds := a.soleOwner(t)
		shared := a.createWorkspace(t, "Shared")
		a.addMember(t, shared, a.userID(t, "dana@example.com"), workspace.RoleOwner)
		a.addMember(t, shared, a.createUser(t, "co@example.com"), workspace.RoleOwner)

		res := a.deleteAccount(t, adminPassword, creds)

		require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
		var refused lastOwnerRefusal
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &refused))
		assert.Equal(t, "LAST_OWNER", refused.Error.Code)
		require.Len(t, refused.Error.Workspaces, 1, "the one they own alone, and not the one with another owner")
		assert.Equal(t, string(sole), refused.Error.Workspaces[0].ID)
		assert.Equal(t, "Dana's team", refused.Error.Workspaces[0].Name)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, creds).Code, "they are still there")
	})
}

func TestAccountDeletion_OnceTheWorkspaceIsScheduledForDeletionTheOwnerCanDeleteTheirAccount(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		_, sole, creds := a.soleOwner(t)
		require.Equal(t, http.StatusConflict, a.deleteAccount(t, adminPassword, creds).Code)
		require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Dana's team", creds).Code)

		res := a.deleteAccount(t, adminPassword, creds)

		assert.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
		assert.Equal(t, 1, a.footprint(t, seededWorkspace{id: sole})["workspaces"], "the workspace waits out its 7 days, without an owner")
		assert.Equal(t, 1, a.footprint(t, seededWorkspace{id: sole})["workspace_members"], "with the member who is left")
	})
}

func TestAccountDeletion_TheLastInstanceAdminIsRefused(t *testing.T) {
	a := newAppWithAdmin(t)
	// the other owner of the admin's workspace makes the last-owner rule pass, so the refusal is the
	// last instance admin's
	a.addMember(t, a.firstWorkspace(t), a.createUser(t, "co@example.com"), workspace.RoleOwner)

	res := a.deleteAccount(t, adminPassword, a.asAdmin)

	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	assert.Contains(t, res.Body.String(), "The install needs at least one instance admin", "as the admin area says it")
	assert.Equal(t, 2, a.listUsers(t, "").Total, "nothing was deleted")

	// with another instance admin they can go
	opsID := a.createUser(t, "ops@example.com")
	require.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}).Code)
	assert.Equal(t, http.StatusNoContent, a.deleteAccount(t, adminPassword, a.asAdmin).Code)
}

func TestAccountDeletion_TheirMembershipsAndSessionsEnd(t *testing.T) {
	parallel(t)
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		leaves := &publishedLeaves{}
		ended := &endedSessions{}
		a := newAppDecorated(t, func(c *dig.Container) error {
			return c.Decorate(func(bus events.EventBus, db *bun.DB) events.EventBus {
				return &endingBus{EventBus: &recordingBus{EventBus: bus, db: func() *bun.DB { return db }, leaves: leaves}, db: db, ended: ended}
			})
		}, backend)
		a.registerAdmin(t)
		first := a.firstWorkspace(t)
		second := a.createWorkspace(t, "Second")
		userID := a.createUser(t, "dana@example.com")
		a.addMember(t, first, userID, workspace.RoleMember)
		a.addMember(t, second, userID, workspace.RoleAdmin)
		a.addMember(t, second, a.createUser(t, "co@example.com"), workspace.RoleOwner)
		held := a.sessionOf(t, "dana@example.com")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)

		require.Equal(t, http.StatusNoContent, a.deleteAccount(t, adminPassword, bearer(held.AccessToken)).Code)

		var members int
		require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
			return db.NewSelect().Table("workspace_members").ColumnExpr("count(*)").Where("user_id = ?", userID).Scan(context.Background(), &members)
		}))
		assert.Zero(t, members, "the memberships went with the user")
		assert.Equal(t, 1, a.footprint(t, seededWorkspace{id: first})["workspaces"], "the workspaces stay")
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code, "the token no longer works")
		assert.Equal(t, http.StatusUnauthorized, a.refreshWith(t, held.RefreshToken).Code)
		assert.Equal(t, http.StatusUnauthorized, a.login(t, userLogin("dana@example.com")).Code)
		published, gone := leaves.snapshot()
		assert.ElementsMatch(t, []workspace.MemberLeftEvent{{WorkspaceID: first, UserID: userID}, {WorkspaceID: second, UserID: userID}}, published)
		assert.Equal(t, []bool{true, true}, gone, "they were gone when it was announced")
		users, states := ended.snapshot()
		assert.Equal(t, []string{userID}, users)
		assert.Equal(t, []string{"gone"}, states, "the sessions ended once the user was gone")
	})
}

func TestAccountDeletion_AnInstanceAdminDeletingTheUserSkipsAWorkspaceScheduledForDeletionToo(t *testing.T) {
	a := newAppWithAdmin(t)
	_, _, creds := a.soleOwner(t)
	userID := a.userID(t, "dana@example.com")
	require.Equal(t, http.StatusConflict, a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin).Code)
	require.Equal(t, http.StatusOK, a.scheduleDeletion(t, "Dana's team", creds).Code)

	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin).Code)
}

func TestAccountDeletion_IsLoggedWithoutThePassword(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	creds := a.userIn(t, "dana@example.com", a.firstWorkspace(t), workspace.RoleMember)
	userID := a.userID(t, "dana@example.com")

	require.Equal(t, http.StatusBadRequest, a.deleteAccount(t, "wrong", creds).Code)
	assert.Empty(t, logs.FilterMessage("Account action").All(), "a refused deletion logs nothing")
	require.Equal(t, http.StatusNoContent, a.deleteAccount(t, adminPassword, creds).Code)

	entries := logs.FilterMessage("Account action").All()
	require.Len(t, entries, 1)
	fields := entries[0].ContextMap()
	assert.Len(t, fields, 2, "who and what, and nothing else")
	assert.Equal(t, userID, fields["actorUserId"])
	assert.Equal(t, "delete_account", fields["action"])
}

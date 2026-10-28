package apitest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/workspace"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// PP-371: the admin area's users (docs/adr/0005 section 10).

const adminUsersPath = "/api/v1/admin/users"

type adminUserWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type adminUser struct {
	ID               string               `json:"id"`
	Email            string               `json:"email"`
	InstanceAdmin    bool                 `json:"instanceAdmin"`
	Active           bool                 `json:"active"`
	TwoFactorEnabled bool                 `json:"twoFactorEnabled"`
	EmailVerified    bool                 `json:"emailVerified"`
	CreatedAt        string               `json:"createdAt"`
	LastLoginAt      *string              `json:"lastLoginAt"`
	Workspaces       []adminUserWorkspace `json:"workspaces"`
}

type adminUsersPage struct {
	Users []adminUser `json:"users"`
	Total int         `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

type adminResetLink struct {
	Link      string    `json:"link"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// listUsers lists the users as the admin, with the query string
func (a *app) listUsers(t *testing.T, query string) adminUsersPage {
	t.Helper()
	return decode[adminUsersPage](t, a.request(t, http.MethodGet, adminUsersPath+query, nil, a.asAdmin), http.StatusOK)
}

// adminView is the user with the email, as the admin list shows them
func (a *app) adminView(t *testing.T, email string) adminUser {
	t.Helper()
	for _, u := range a.listUsers(t, "?limit=100").Users {
		if u.Email == email {
			return u
		}
	}
	t.Fatalf("the admin list does not show %s", email)
	return adminUser{}
}

func (a *app) patchUser(t *testing.T, userID string, body map[string]any, creds ...credentials) *httpRecorder {
	t.Helper()
	if len(creds) == 0 {
		creds = []credentials{a.asAdmin}
	}
	return a.request(t, http.MethodPatch, adminUsersPath+"/"+userID, body, creds...)
}

// userLogin is the login of a user created by createUser
func userLogin(email string) map[string]any {
	return map[string]any{"email": email, "password": adminPassword}
}

type session struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func (a *app) sessionOf(t *testing.T, email string) session {
	t.Helper()
	return decode[session](t, a.login(t, userLogin(email)), http.StatusOK)
}

func (a *app) refreshWith(t *testing.T, token string) *httpRecorder {
	t.Helper()
	return a.request(t, http.MethodPost, "/api/v1/auth/refresh", map[string]string{"refreshToken": token})
}

// MARK: Access

// every admin route, for a user whose ID is given
func adminRoutes(userID string) []call {
	return []call{
		{"list users", http.MethodGet, adminUsersPath, nil},
		{"change a user", http.MethodPatch, adminUsersPath + "/" + userID, map[string]any{"active": true}},
		{"make a reset link", http.MethodPost, adminUsersPath + "/" + userID + "/reset-link", nil},
		{"turn off 2FA", http.MethodPost, adminUsersPath + "/" + userID + "/disable-2fa", nil},
		{"delete a user", http.MethodDelete, adminUsersPath + "/" + userID, nil},
	}
}

func TestAdminUsers_OnlyAnInstanceAdminSignedInAsAUserReachesThem(t *testing.T) {
	a := newAppWithAdmin(t)
	// Even the owner of a workspace manages only that workspace
	owner := a.userIn(t, "owner@example.com", a.firstWorkspace(t), workspace.RoleOwner)
	target := a.createUser(t, "target@example.com")

	for _, route := range adminRoutes(target) {
		t.Run(route.name+"/owner without the flag", func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, owner)
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
		t.Run(route.name+"/API key", func(t *testing.T) {
			w := a.request(t, route.method, route.path, route.body, a.withAPIKey)
			assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
		t.Run(route.name+"/unauthenticated", func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, a.request(t, route.method, route.path, route.body).Code)
		})
	}

	// and nothing changed
	assert.Equal(t, 3, a.listUsers(t, "").Total)
	assert.True(t, a.adminView(t, "target@example.com").Active)
}

func TestAdminUsers_AnInstanceAdminWithoutAWorkspaceReachesThem(t *testing.T) {
	a := newAppWithAdmin(t)
	opsID := a.createUser(t, "ops@example.com")
	a.setInstanceAdmin(t, opsID, true)
	ops := a.loginAs(t, "ops@example.com")

	page := decode[adminUsersPage](t, a.request(t, http.MethodGet, adminUsersPath, nil, ops), http.StatusOK)
	assert.Equal(t, 2, page.Total)
}

// MARK: List and search

func TestAdminUsers_ListShowsEveryUserWithTheirWorkspacesAndState(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		first := a.firstWorkspace(t)
		second := a.createWorkspace(t, "Second")
		bobID := a.createUser(t, "bob@example.com")
		a.addMember(t, first, bobID, workspace.RoleMember)
		a.addMember(t, second, bobID, workspace.RoleOwner)
		a.createUser(t, "nobody@example.com") // in no workspace at all
		bob := a.sessionOf(t, "bob@example.com")
		require.NotEmpty(t, bob.AccessToken)

		page := a.listUsers(t, "")
		assert.Equal(t, 3, page.Total)
		require.Len(t, page.Users, 3)
		assert.Equal(t, []string{adminEmail, "bob@example.com", "nobody@example.com"}, []string{page.Users[0].Email, page.Users[1].Email, page.Users[2].Email}, "the oldest first")

		admin := a.adminView(t, adminEmail)
		assert.True(t, admin.InstanceAdmin)
		assert.True(t, admin.Active)
		assert.False(t, admin.TwoFactorEnabled)
		assert.NotEmpty(t, admin.CreatedAt)
		require.Len(t, admin.Workspaces, 1)
		assert.Equal(t, adminUserWorkspace{ID: string(first), Name: "My workspace", Role: "owner"}, adminUserWorkspace{ID: admin.Workspaces[0].ID, Name: "My workspace", Role: admin.Workspaces[0].Role})
		require.NotNil(t, admin.LastLoginAt, "signing up signed the first user in")

		bobView := a.adminView(t, "bob@example.com")
		assert.False(t, bobView.InstanceAdmin)
		assert.False(t, bobView.EmailVerified, "createUser leaves the address unverified")
		require.NotNil(t, bobView.LastLoginAt, "bob logged in")
		assert.WithinDuration(t, time.Now(), mustParse(t, *bobView.LastLoginAt), time.Minute)
		require.Len(t, bobView.Workspaces, 2)
		assert.Equal(t, adminUserWorkspace{ID: string(first), Name: bobView.Workspaces[0].Name, Role: "member"}, bobView.Workspaces[0], "the workspace they joined first comes first")
		assert.Equal(t, adminUserWorkspace{ID: string(second), Name: "Second", Role: "owner"}, bobView.Workspaces[1])

		nobody := a.adminView(t, "nobody@example.com")
		assert.Nil(t, nobody.LastLoginAt, "never signed in")
		assert.NotNil(t, nobody.Workspaces)
		assert.Empty(t, nobody.Workspaces)
	})
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	require.NoError(t, err)
	return parsed
}

func TestAdminUsers_SearchFindsAnEmailIgnoringCaseAndTakesWildcardsLiterally(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		for _, email := range []string{"Alice@Example.com", "alice.smith@corp.test", "bob_x@example.com", "bobby@example.com"} {
			a.createUser(t, email)
		}
		emails := func(query string) []string {
			var out []string
			for _, u := range a.listUsers(t, query).Users {
				out = append(out, u.Email)
			}
			return out
		}

		assert.Equal(t, []string{"Alice@Example.com", "alice.smith@corp.test"}, emails("?q=ALICE"))
		assert.Equal(t, []string{"alice.smith@corp.test"}, emails("?q=corp.test"))
		assert.Equal(t, []string{"bob_x@example.com"}, emails("?q=bob_x"), "an underscore is no wildcard")
		assert.Empty(t, emails("?q=%25"), "a percent sign is no wildcard")
		assert.Empty(t, emails("?q=nobody"))
		assert.Equal(t, 2, a.listUsers(t, "?q=alice").Total)
	})
}

func TestAdminUsers_ListIsPaged(t *testing.T) {
	a := newAppWithAdmin(t)
	for i := range 4 {
		a.createUser(t, fmt.Sprintf("user%d@example.com", i))
	}

	first := a.listUsers(t, "?limit=2")
	assert.Equal(t, 5, first.Total)
	require.Len(t, first.Users, 2)
	assert.Equal(t, adminEmail, first.Users[0].Email)
	last := a.listUsers(t, "?limit=2&page=2")
	require.Len(t, last.Users, 1)
	assert.Equal(t, "user3@example.com", last.Users[0].Email)
	assert.Equal(t, 2, last.Page)
	assert.Equal(t, 2, last.Limit)
	assert.Empty(t, a.listUsers(t, "?limit=2&page=3").Users)
	assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodGet, adminUsersPath+"?limit=0", nil, a.asAdmin).Code)
	assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodGet, adminUsersPath+"?limit=101", nil, a.asAdmin).Code)
	assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodGet, adminUsersPath+"?page=-1", nil, a.asAdmin).Code)
}

func TestAdminUsers_LastSignInIsWhenTheyLastLoggedIn(t *testing.T) {
	a := newAppWithAdmin(t)
	a.createUser(t, "late@example.com")
	require.Nil(t, a.adminView(t, "late@example.com").LastLoginAt)

	a.sessionOf(t, "late@example.com")
	first := a.adminView(t, "late@example.com").LastLoginAt
	require.NotNil(t, first)

	time.Sleep(20 * time.Millisecond)
	// a failed login is no sign-in
	assert.Equal(t, http.StatusUnauthorized, a.login(t, map[string]any{"email": "late@example.com", "password": "Wrong-password-1"}).Code)
	assert.Equal(t, *first, *a.adminView(t, "late@example.com").LastLoginAt)

	a.sessionOf(t, "late@example.com")
	assert.True(t, mustParse(t, *a.adminView(t, "late@example.com").LastLoginAt).After(mustParse(t, *first)))
}

// MARK: The instance admin flag

func TestAdminUsers_GiveAndTakeTheInstanceAdminFlag(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		opsID := a.createUser(t, "ops@example.com")
		ops := a.loginAs(t, "ops@example.com")
		require.Equal(t, http.StatusForbidden, a.request(t, http.MethodGet, adminUsersPath, nil, ops).Code)

		given := decode[adminUser](t, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}), http.StatusOK)
		assert.True(t, given.InstanceAdmin)
		assert.True(t, a.adminView(t, "ops@example.com").InstanceAdmin)
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, adminUsersPath, nil, ops).Code, "the token they already hold now opens the admin area")
		assert.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}).Code, "giving it again changes nothing")

		taken := decode[adminUser](t, a.patchUser(t, opsID, map[string]any{"instanceAdmin": false}), http.StatusOK)
		assert.False(t, taken.InstanceAdmin)
		assert.Equal(t, http.StatusForbidden, a.request(t, http.MethodGet, adminUsersPath, nil, ops).Code)
	})
}

func TestAdminUsers_TheLastInstanceAdminKeepsTheFlag(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		adminID := a.userID(t, adminEmail)

		w := a.patchUser(t, adminID, map[string]any{"instanceAdmin": false})
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.True(t, a.adminView(t, adminEmail).InstanceAdmin)

		// with a second instance admin the first may hand theirs over, and the second is last
		opsID := a.createUser(t, "ops@example.com")
		require.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}).Code)
		ops := a.loginAs(t, "ops@example.com")
		require.Equal(t, http.StatusOK, a.patchUser(t, adminID, map[string]any{"instanceAdmin": false}).Code, "an admin may take their own flag when another has it")
		assert.Equal(t, http.StatusConflict, a.patchUser(t, opsID, map[string]any{"instanceAdmin": false}, ops).Code, "the one who is left can't lose it")
		stillAdmin := decode[adminUsersPage](t, a.request(t, http.MethodGet, adminUsersPath+"?q=ops@", nil, ops), http.StatusOK)
		require.Len(t, stillAdmin.Users, 1)
		assert.True(t, stillAdmin.Users[0].InstanceAdmin)
	})
}

func TestAdminUsers_AChangeNeedsAFieldAndAnExistingUser(t *testing.T) {
	a := newAppWithAdmin(t)
	adminID := a.userID(t, adminEmail)

	assert.Equal(t, http.StatusBadRequest, a.patchUser(t, adminID, map[string]any{}).Code)
	assert.Equal(t, http.StatusNotFound, a.patchUser(t, "00000000-0000-4000-8000-000000000000", map[string]any{"active": true}).Code)
	assert.Equal(t, http.StatusNotFound, a.patchUser(t, "not-a-uuid", map[string]any{"active": true}).Code)
	for _, route := range adminRoutes("00000000-0000-4000-8000-000000000000")[2:] {
		t.Run(route.name, func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, a.request(t, route.method, route.path, route.body, a.asAdmin).Code)
		})
	}
}

// MARK: Disable and enable

func TestAdminUsers_ADisabledUserCanNotSignInAndTheirSessionsEnd(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		second := a.createWorkspace(t, "Second")
		userID := a.createUser(t, "user@example.com")
		a.addMember(t, second, userID, workspace.RoleOwner)
		colleague := a.userIn(t, "colleague@example.com", second, workspace.RoleOwner)
		held := a.sessionOf(t, "user@example.com")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)
		monitor := decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", pushMonitorBody("Runs on", "second-push-token"), colleague, inWorkspace(second)), http.StatusCreated).ID

		disabled := decode[adminUser](t, a.patchUser(t, userID, map[string]any{"active": false}), http.StatusOK)
		assert.False(t, disabled.Active)
		assert.False(t, a.adminView(t, "user@example.com").Active)

		// the sessions they held end
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)
		assert.Equal(t, http.StatusUnauthorized, a.refreshWith(t, held.RefreshToken).Code)
		// and they can't start another: with the right password the answer says why, and with a wrong one it doesn't
		denied := a.login(t, userLogin("user@example.com"))
		assert.Equal(t, http.StatusUnauthorized, denied.Code)
		assert.Contains(t, denied.Body.String(), "disabled")
		wrong := a.login(t, map[string]any{"email": "user@example.com", "password": "Wrong-password-1"})
		assert.Equal(t, http.StatusUnauthorized, wrong.Code)
		assert.NotContains(t, wrong.Body.String(), "disabled")

		// their workspace runs on: the monitor is still active, the workspace is not suspended, and their place is kept
		stored := decode[struct {
			Active bool `json:"active"`
		}](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+monitor, nil, colleague, inWorkspace(second)), http.StatusOK)
		assert.True(t, stored.Active)
		for _, w := range a.meOf(t, colleague).Workspaces {
			assert.False(t, w.Suspended, w.Name)
		}
		require.Len(t, a.adminView(t, "user@example.com").Workspaces, 1)
		assert.Equal(t, "owner", a.adminView(t, "user@example.com").Workspaces[0].Role)
	})
}

func TestAdminUsers_ADisabledUserSignsInAgainOnceEnabled(t *testing.T) {
	a := newAppWithAdmin(t)
	userID := a.createUser(t, "user@example.com")
	held := a.sessionOf(t, "user@example.com")

	require.Equal(t, http.StatusOK, a.patchUser(t, userID, map[string]any{"active": false}).Code)
	require.Equal(t, http.StatusUnauthorized, a.login(t, userLogin("user@example.com")).Code)
	require.Equal(t, http.StatusOK, a.patchUser(t, userID, map[string]any{"active": true}).Code)

	assert.True(t, a.adminView(t, "user@example.com").Active)
	again := a.sessionOf(t, "user@example.com")
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(again.AccessToken)).Code)
	assert.Equal(t, http.StatusOK, a.refreshWith(t, again.RefreshToken).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code, "the session from before stays ended")
}

func TestAdminUsers_AnAdminCanNotDisableThemselves(t *testing.T) {
	a := newAppWithAdmin(t)
	adminID := a.userID(t, adminEmail)

	w := a.patchUser(t, adminID, map[string]any{"active": false})
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.True(t, a.adminView(t, adminEmail).Active)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, a.asAdmin).Code)
}

func TestAdminUsers_ADisabledInstanceAdminLosesTheAdminArea(t *testing.T) {
	a := newAppWithAdmin(t)
	opsID := a.createUser(t, "ops@example.com")
	require.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}).Code)
	ops := a.loginAs(t, "ops@example.com")
	require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, adminUsersPath, nil, ops).Code)

	require.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"active": false}).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, adminUsersPath, nil, ops).Code)
}

// MARK: Reset link

func TestAdminUsers_AResetLinkWorksOnceWithoutAnEmailSender(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend, selfHosted)
		require.False(t, a.cfg.AccountEmailsEnabled(), "this install has no sender")
		userID := a.createUser(t, "user@example.com")
		held := a.sessionOf(t, "user@example.com")

		w := a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/reset-link", nil, a.asAdmin)
		link := decode[adminResetLink](t, w, http.StatusCreated)
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.True(t, strings.HasPrefix(link.Link, clientURL+"/reset-password/pkr_"), link.Link)
		assert.WithinDuration(t, time.Now().Add(time.Hour), link.ExpiresAt, time.Minute, "it expires in an hour")
		token := strings.TrimPrefix(link.Link, clientURL+"/reset-password/")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)

		const newPassword = "Brand-new-pass-9"
		reset := a.request(t, http.MethodPost, resetPath, map[string]string{"token": token, "password": newPassword})
		require.Equal(t, http.StatusOK, reset.Code, reset.Body.String())

		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code, "using the link ends the user's sessions")
		assert.Equal(t, http.StatusUnauthorized, a.refreshWith(t, held.RefreshToken).Code)
		assert.Equal(t, http.StatusUnauthorized, a.login(t, userLogin("user@example.com")).Code, "the old password is gone")
		assert.Equal(t, http.StatusOK, a.login(t, map[string]any{"email": "user@example.com", "password": newPassword}).Code)
		again := a.request(t, http.MethodPost, resetPath, map[string]string{"token": token, "password": "Another-pass-77"})
		assert.Equal(t, http.StatusBadRequest, again.Code, "the link works once")
	})
}

func TestAdminUsers_AResetLinkExpiresAndAFreshOneReplacesNothing(t *testing.T) {
	a := newAppWithAdmin(t, selfHosted)
	userID := a.createUser(t, "user@example.com")
	make := func() string {
		link := decode[adminResetLink](t, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/reset-link", nil, a.asAdmin), http.StatusCreated)
		return strings.TrimPrefix(link.Link, clientURL+"/reset-password/")
	}
	expired, live := make(), make()
	require.NotEqual(t, expired, live)
	sum := sha256.Sum256([]byte(expired))
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.Exec("UPDATE account_tokens SET expires_at = ? WHERE token_hash = ?", time.Now().Add(-time.Minute).UTC(), "sha256$"+hex.EncodeToString(sum[:]))
		return err
	}))

	assert.Equal(t, http.StatusBadRequest, a.request(t, http.MethodPost, resetPath, map[string]string{"token": expired, "password": "Brand-new-pass-9"}).Code)
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, resetPath, map[string]string{"token": live, "password": "Brand-new-pass-9"}).Code, "an expired link doesn't spoil the next")
}

// MARK: 2FA

func TestAdminUsers_AnAdminTurnsOffAUsersTwoFactor(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		userID := a.createUser(t, "user@example.com")
		user := bearer(a.sessionOf(t, "user@example.com").AccessToken)

		w := a.request(t, http.MethodPost, "/api/v1/auth/2fa/setup", map[string]any{"email": "user@example.com", "password": adminPassword}, user)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var setup struct {
			Secret string `json:"secret"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &setup))
		code, err := totp.GenerateCode(setup.Secret, time.Now())
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, "/api/v1/auth/2fa/verify", map[string]any{"email": "user@example.com", "code": code}, user).Code)
		require.True(t, a.adminView(t, "user@example.com").TwoFactorEnabled)
		require.Equal(t, http.StatusUnauthorized, a.login(t, userLogin("user@example.com")).Code, "the password alone no longer signs in")

		off := decode[adminUser](t, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/disable-2fa", nil, a.asAdmin), http.StatusOK)
		assert.False(t, off.TwoFactorEnabled)
		assert.False(t, a.adminView(t, "user@example.com").TwoFactorEnabled)
		assert.Equal(t, http.StatusOK, a.login(t, userLogin("user@example.com")).Code, "the password is enough again")
		assert.Equal(t, http.StatusOK, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/disable-2fa", nil, a.asAdmin).Code, "a user without 2FA stays as they are")
	})
}

// MARK: Delete

func TestAdminUsers_DeleteIsRefusedForTheLastOwnerOfAWorkspaceAndNamesIt(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		sole := a.createWorkspace(t, "Only mine")
		shared := a.createWorkspace(t, "Shared")
		userID := a.createUser(t, "user@example.com")
		a.addMember(t, sole, userID, workspace.RoleOwner)
		a.addMember(t, shared, userID, workspace.RoleOwner)
		a.addMember(t, shared, a.createUser(t, "co@example.com"), workspace.RoleOwner)

		w := a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `\"Only mine\"`, "it names the workspace they are the last owner of")
		assert.NotContains(t, w.Body.String(), "Shared", "and not the one with another owner")
		assert.Equal(t, 3, a.listUsers(t, "").Total, "nothing was deleted")
		assert.Len(t, a.adminView(t, "user@example.com").Workspaces, 2)

		// with another owner it goes through
		a.addMember(t, sole, a.userID(t, "co@example.com"), workspace.RoleOwner)
		assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin).Code)
	})
}

func TestAdminUsers_DeleteRemovesTheUserAndTheirMembershipsAndAnnouncesThem(t *testing.T) {
	a, leaves := newAppRecordingLeaves(t)
	first := a.firstWorkspace(t)
	second := a.createWorkspace(t, "Second")
	userID := a.createUser(t, "user@example.com")
	a.addMember(t, first, userID, workspace.RoleMember)
	a.addMember(t, second, userID, workspace.RoleAdmin)
	a.addMember(t, second, a.createUser(t, "co@example.com"), workspace.RoleOwner)
	resetLink := decode[adminResetLink](t, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/reset-link", nil, a.asAdmin), http.StatusCreated)
	require.NotEmpty(t, resetLink.Link)

	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin).Code)

	assert.Equal(t, 2, a.listUsers(t, "").Total)
	assert.Empty(t, a.listUsers(t, "?q=user@example").Users)
	var members, tokens int
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		ctx := context.Background()
		if err := db.NewSelect().Table("workspace_members").ColumnExpr("count(*)").Where("user_id = ?", userID).Scan(ctx, &members); err != nil {
			return err
		}
		return db.NewSelect().Table("account_tokens").ColumnExpr("count(*)").Where("user_id = ?", userID).Scan(ctx, &tokens)
	}))
	assert.Zero(t, members, "the memberships went with the user")
	assert.Zero(t, tokens, "and so did their links")
	assert.Equal(t, http.StatusUnauthorized, a.login(t, userLogin("user@example.com")).Code)

	published, gone := leaves.snapshot()
	assert.ElementsMatch(t, []workspace.MemberLeftEvent{{WorkspaceID: first, UserID: userID}, {WorkspaceID: second, UserID: userID}}, published)
	assert.Equal(t, []bool{true, true}, gone, "they were gone when it was announced")
}

func TestAdminUsers_ADeletedUsersTokensStopWorkingAtOnce(t *testing.T) {
	forEachBackend(t, func(t *testing.T, backend func(*config.Config)) {
		a := newAppWithAdmin(t, backend)
		first := a.firstWorkspace(t)
		userID := a.createUser(t, "user@example.com")
		a.addMember(t, first, userID, workspace.RoleMember)
		held := a.sessionOf(t, "user@example.com")
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)
		require.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(held.AccessToken), inWorkspace(first)).Code)

		require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+userID, nil, a.asAdmin).Code)

		// The token has not expired, but its user is gone
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/me", nil, bearer(held.AccessToken)).Code)
		assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/monitors", nil, bearer(held.AccessToken), inWorkspace(first)).Code)
		assert.Equal(t, http.StatusUnauthorized, a.refreshWith(t, held.RefreshToken).Code)
	})
}

func TestAdminUsers_AnAdminCanNotDeleteThemselves(t *testing.T) {
	a, leaves := newAppRecordingLeaves(t)
	adminID := a.userID(t, adminEmail)

	assert.Equal(t, http.StatusConflict, a.request(t, http.MethodDelete, adminUsersPath+"/"+adminID, nil, a.asAdmin).Code)
	assert.Equal(t, 1, a.listUsers(t, "").Total)
	published, _ := leaves.snapshot()
	assert.Empty(t, published, "a refused delete announces nothing")
}

func TestAdminUsers_AnotherInstanceAdminCanBeDeleted(t *testing.T) {
	a := newAppWithAdmin(t)
	opsID := a.createUser(t, "ops@example.com")
	require.Equal(t, http.StatusOK, a.patchUser(t, opsID, map[string]any{"instanceAdmin": true}).Code)

	assert.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+opsID, nil, a.asAdmin).Code)
	assert.Equal(t, 1, a.listUsers(t, "").Total)
}

// MARK: Log

func TestAdminUsers_EveryActionLogsWhoDidItToWhomAndNoSecret(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	a := newAppDecorated(t, func(container *dig.Container) error {
		return container.Decorate(func(*zap.SugaredLogger) *zap.SugaredLogger { return zap.New(core).Sugar() })
	}, selfHosted)
	a.registerAdmin(t)
	adminID := a.userID(t, adminEmail)
	userID := a.createUser(t, "user@example.com")
	victimID := a.createUser(t, "victim@example.com")

	link := decode[adminResetLink](t, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/reset-link", nil, a.asAdmin), http.StatusCreated)
	require.Equal(t, http.StatusOK, a.patchUser(t, userID, map[string]any{"instanceAdmin": true}).Code)
	require.Equal(t, http.StatusOK, a.patchUser(t, userID, map[string]any{"active": false}).Code)
	require.Equal(t, http.StatusOK, a.patchUser(t, userID, map[string]any{"active": true}).Code)
	require.Equal(t, http.StatusOK, a.request(t, http.MethodPost, adminUsersPath+"/"+userID+"/disable-2fa", nil, a.asAdmin).Code)
	require.Equal(t, http.StatusNoContent, a.request(t, http.MethodDelete, adminUsersPath+"/"+victimID, nil, a.asAdmin).Code)

	type line struct{ actor, action, target string }
	var got []line
	for _, entry := range logs.FilterMessage("Admin action").All() {
		fields := entry.ContextMap()
		assert.Len(t, fields, 3, "who, what and to whom, and nothing else")
		assert.Equal(t, zap.InfoLevel, entry.Level)
		got = append(got, line{fields["actorUserId"].(string), fields["action"].(string), fields["targetUserId"].(string)})
		assert.NotContains(t, fmt.Sprint(fields), strings.TrimPrefix(link.Link, clientURL+"/reset-password/"))
	}
	assert.Equal(t, []line{
		{adminID, "create_reset_link", userID},
		{adminID, "set_instance_admin", userID},
		{adminID, "disable_user", userID},
		{adminID, "enable_user", userID},
		{adminID, "disable_2fa", userID},
		{adminID, "delete_user", victimID},
	}, got)

	// a refused action logs nothing
	logs.TakeAll()
	require.Equal(t, http.StatusConflict, a.patchUser(t, adminID, map[string]any{"active": false}).Code)
	assert.Empty(t, logs.FilterMessage("Admin action").All())
}

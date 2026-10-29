package setting_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/httpx"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/utils"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// acceptingAPIKeys accepts every API key, standing in for a stored one.
type acceptingAPIKeys struct {
	api_key.Service
}

func (acceptingAPIKeys) ValidateKey(context.Context, string) (*api_key.Model, error) {
	return &api_key.Model{ID: "api-key-1", WorkspaceID: testWorkspace}, nil
}

// testWorkspace is the workspace of the test user and of the API key
const testWorkspace = workspace.ID("5b7f2a64-0c1d-4e3a-9a52-1f6d8c3b7e01")

// otherWorkspace is a second workspace the test user is in
const otherWorkspace = workspace.ID("8c2e4d19-7a3b-4f60-b1d5-9e0a2c6f4d02")

// memberOf puts every user in testWorkspace with the role, and in otherWorkspace as an owner
type memberOf struct{ role workspace.Role }

func (m memberOf) ListMemberships(context.Context, string) ([]workspace.Membership, error) {
	return []workspace.Membership{
		{Workspace: workspace.Workspace{ID: testWorkspace}, Role: m.role},
		{Workspace: workspace.Workspace{ID: otherWorkspace}, Role: workspace.RoleOwner},
	}, nil
}

// settingsAPI serves the settings routes of an upgraded install behind the
// real auth chain.
type settingsAPI struct {
	engine      *httpx.Router
	settings    setting.Service
	workspace   setting.WorkspaceService
	accessToken string
}

func newSettingsAPI(t *testing.T) *settingsAPI {
	t.Helper()
	return newSettingsAPIAs(t, workspace.RoleOwner)
}

// newSettingsAPIAs serves the settings routes to a user who holds the role in testWorkspace
func newSettingsAPIAs(t *testing.T, role workspace.Role) *settingsAPI {
	t.Helper()

	ctx := context.Background()
	logger := zap.NewNop().Sugar()

	settings, tokenMaker := newSettingsWithTokens()
	seedEarlierRelease(t, settings)
	require.NoError(t, settings.InitializeSettings(ctx))

	accessToken, err := tokenMaker.CreateAccessToken(ctx, testUser)
	require.NoError(t, err)

	authChain := middleware.NewAuthChain(
		auth.NewMiddlewareProvider(tokenMaker),
		api_key.NewMiddlewareProvider(acceptingAPIKeys{}),
		memberOf{role},
		nil,
		nil,
		logger,
	)
	workspaceSettings := setting.NewWorkspaceService(newMemoryWorkspaceRepository())
	controller := setting.NewController(setting.NewAPIService(workspaceSettings), logger)
	engine := httpx.NewRouter()
	setting.NewRoute(controller, authChain).ConnectRoute(engine.Group("/api/v1"), controller)

	return &settingsAPI{engine: engine, settings: settings, workspace: workspaceSettings, accessToken: accessToken}
}

var authMethods = []struct {
	name      string
	authorize func(req *http.Request, api *settingsAPI)
}{
	{"jwt", func(req *http.Request, api *settingsAPI) { req.Header.Set("Authorization", "Bearer "+api.accessToken) }},
	{"api_key", func(req *http.Request, _ *settingsAPI) { req.Header.Set("X-API-Key", "pk_test") }},
}

func (api *settingsAPI) request(method, key, body string, authorize func(*http.Request, *settingsAPI)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/settings/key/"+key, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	authorize(req, api)
	w := httptest.NewRecorder()
	api.engine.ServeHTTP(w, req)
	return w
}

// stored reads a setting the way the rest of the server does, bypassing the API.
func (api *settingsAPI) stored(t *testing.T, key string) string {
	t.Helper()
	model, err := api.settings.GetByKey(context.Background(), key)
	require.NoError(t, err)
	if model == nil {
		return ""
	}
	return model.Value
}

func TestSettingsAPI_HidesInternalSettings(t *testing.T) {
	internalKeys := []string{
		shared.AccessTokenSecretKey,
		shared.RefreshTokenSecretKey,
		"ACCESS_TOKEN_SECRET_KEY",
		"REFRESH_TOKEN_SECRET_KEY",
		"ACCESS_TOKEN_EXPIRED_IN",
		"REFRESH_TOKEN_EXPIRED_IN",
	}
	// A PUT with a bad body must get 404 too, not the 400 a user-editable key
	// would get.
	requests := []struct {
		name, method, body string
	}{
		{"GET", http.MethodGet, ""},
		{"PUT", http.MethodPut, `{"value":"attacker-chosen","type":"string"}`},
		{"PUT_malformed_body", http.MethodPut, `{`},
		{"PUT_invalid_body", http.MethodPut, `{"value":"attacker-chosen"}`},
		{"DELETE", http.MethodDelete, ""},
	}
	for _, as := range authMethods {
		for _, key := range internalKeys {
			for _, req := range requests {
				t.Run(as.name+"/"+req.name+"/"+key, func(t *testing.T) {
					api := newSettingsAPI(t)
					before := api.stored(t, key)
					require.NotEmpty(t, before)

					w := api.request(req.method, key, req.body, as.authorize)

					assert.Equal(t, http.StatusNotFound, w.Code)
					assert.NotContains(t, w.Body.String(), before)
					assert.Equal(t, before, api.stored(t, key))
				})
			}
		}
	}
}

// requestIn is request in a workspace the user chose with the X-Workspace-ID header
func (api *settingsAPI) requestIn(id workspace.ID, method, key, body string, authorize func(*http.Request, *settingsAPI)) *httptest.ResponseRecorder {
	return api.request(method, key, body, func(req *http.Request, api *settingsAPI) {
		authorize(req, api)
		req.Header.Set("X-Workspace-ID", string(id))
	})
}

func TestSettingsAPI_ServesDataRetentionPeriod(t *testing.T) {
	for _, as := range authMethods {
		t.Run(as.name, func(t *testing.T) {
			api := newSettingsAPI(t)

			w := api.request(http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{`, as.authorize)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

			w = api.request(http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"30","type":"int"}`, as.authorize)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			w = api.request(http.MethodGet, "KEEP_DATA_PERIOD_DAYS", "", as.authorize)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var got utils.ApiResponse[setting.Model]
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, "30", got.Data.Value)

			w = api.request(http.MethodDelete, "KEEP_DATA_PERIOD_DAYS", "", as.authorize)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			w = api.request(http.MethodGet, "KEEP_DATA_PERIOD_DAYS", "", as.authorize)
			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestSettingsAPI_AMemberReadsTheDataRetentionPeriodAndCannotChangeIt(t *testing.T) {
	api := newSettingsAPIAs(t, workspace.RoleMember)
	_, err := api.workspace.SetByKey(context.Background(), testWorkspace, "KEEP_DATA_PERIOD_DAYS", &setting.Model{Value: "30", Type: "int"})
	require.NoError(t, err)

	w := api.request(http.MethodGet, "KEEP_DATA_PERIOD_DAYS", "", authMethods[0].authorize)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got utils.ApiResponse[setting.Model]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "30", got.Data.Value)

	w = api.request(http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"1","type":"int"}`, authMethods[0].authorize)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	w = api.request(http.MethodDelete, "KEEP_DATA_PERIOD_DAYS", "", authMethods[0].authorize)
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())

	stored, err := api.workspace.GetByKey(context.Background(), testWorkspace, "KEEP_DATA_PERIOD_DAYS")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "30", stored.Value, "the member's change did nothing")
}

func TestSettingsAPI_AnAdminAnOwnerAndAnAPIKeyChangeTheDataRetentionPeriod(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      workspace.Role
		authorize func(req *http.Request, api *settingsAPI)
	}{
		{"admin", workspace.RoleAdmin, authMethods[0].authorize},
		{"owner", workspace.RoleOwner, authMethods[0].authorize},
		{"api key", workspace.RoleMember, authMethods[1].authorize}, // a key acts as an admin whatever the user's role
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newSettingsAPIAs(t, tc.role)

			w := api.request(http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"12","type":"int"}`, tc.authorize)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			stored, err := api.workspace.GetByKey(context.Background(), testWorkspace, "KEEP_DATA_PERIOD_DAYS")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "12", stored.Value)
			install, err := api.settings.GetByKey(context.Background(), "KEEP_DATA_PERIOD_DAYS")
			require.NoError(t, err)
			assert.Nil(t, install, "and the install's own settings do not hold it")
		})
	}
}

func TestSettingsAPI_EachWorkspaceHoldsItsOwnDataRetentionPeriod(t *testing.T) {
	api := newSettingsAPI(t)
	auth := authMethods[0].authorize

	require.Equal(t, http.StatusOK, api.requestIn(testWorkspace, http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"1","type":"int"}`, auth).Code)
	require.Equal(t, http.StatusOK, api.requestIn(otherWorkspace, http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"10","type":"int"}`, auth).Code)

	for id, want := range map[workspace.ID]string{testWorkspace: "1", otherWorkspace: "10"} {
		w := api.requestIn(id, http.MethodGet, "KEEP_DATA_PERIOD_DAYS", "", auth)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got utils.ApiResponse[setting.Model]
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, want, got.Data.Value, string(id))
	}
}

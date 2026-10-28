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
const testWorkspace = workspace.ID("w-1")

// onlyWorkspace puts every user in testWorkspace
type onlyWorkspace struct{}

func (onlyWorkspace) ListMemberships(context.Context, string) ([]workspace.Membership, error) {
	return []workspace.Membership{{Workspace: workspace.Workspace{ID: testWorkspace}, Role: workspace.RoleOwner}}, nil
}

// settingsAPI serves the settings routes of an upgraded install behind the
// real auth chain.
type settingsAPI struct {
	engine      *httpx.Router
	settings    setting.Service
	accessToken string
}

func newSettingsAPI(t *testing.T) *settingsAPI {
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
		onlyWorkspace{},
		nil,
		nil,
		logger,
	)
	controller := setting.NewController(setting.NewAPIService(settings), logger)
	engine := httpx.NewRouter()
	setting.NewRoute(controller, authChain).ConnectRoute(engine.Group("/api/v1"), controller)

	return &settingsAPI{engine: engine, settings: settings, accessToken: accessToken}
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

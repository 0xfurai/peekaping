package setting_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/middleware"
	"peekaping/internal/modules/setting"
	"peekaping/internal/utils"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// acceptingAPIKeys accepts every API key, standing in for a stored one.
type acceptingAPIKeys struct {
	api_key.Service
}

func (acceptingAPIKeys) ValidateKey(context.Context, string) (*api_key.Model, error) {
	return &api_key.Model{ID: "api-key-1"}, nil
}

// settingsAPI serves the settings routes behind the real auth chain.
type settingsAPI struct {
	engine      *gin.Engine
	settings    setting.Service
	accessToken string
}

func newSettingsAPI(t *testing.T) *settingsAPI {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	logger := zap.NewNop().Sugar()

	settings := setting.NewService(newMemoryRepository(), logger)
	require.NoError(t, settings.InitializeSettings(ctx))

	tokenMaker := auth.NewTokenMaker(settings, logger)
	accessToken, err := tokenMaker.CreateAccessToken(ctx, &auth.Model{ID: "user-1", Email: "admin@example.com"})
	require.NoError(t, err)

	authChain := middleware.NewAuthChain(
		auth.NewMiddlewareProvider(tokenMaker),
		api_key.NewMiddlewareProvider(acceptingAPIKeys{}),
		logger,
	)
	controller := setting.NewController(setting.NewAPIService(settings), logger)
	engine := gin.New()
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
		"ACCESS_TOKEN_SECRET_KEY",
		"REFRESH_TOKEN_SECRET_KEY",
		"ACCESS_TOKEN_EXPIRED_IN",
		"REFRESH_TOKEN_EXPIRED_IN",
	}
	for _, as := range authMethods {
		for _, key := range internalKeys {
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
				t.Run(as.name+"/"+method+"/"+key, func(t *testing.T) {
					api := newSettingsAPI(t)
					before := api.stored(t, key)
					require.NotEmpty(t, before)

					body := ""
					if method == http.MethodPut {
						body = `{"value":"attacker-chosen","type":"string"}`
					}
					w := api.request(method, key, body, as.authorize)

					assert.Equal(t, http.StatusNotFound, w.Code)
					assert.NotContains(t, w.Body.String(), before)
					assert.Equal(t, before, api.stored(t, key))
				})
			}
		}
	}
}

func TestSettingsAPI_ServesKeepDataPeriod(t *testing.T) {
	for _, as := range authMethods {
		t.Run(as.name, func(t *testing.T) {
			api := newSettingsAPI(t)

			w := api.request(http.MethodPut, "KEEP_DATA_PERIOD_DAYS", `{"value":"30","type":"int"}`, as.authorize)
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

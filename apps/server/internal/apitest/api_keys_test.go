package apitest

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type apiKeyResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestAPIKeysAPI_ManagesKey(t *testing.T) {
	a := newAppWithAdmin(t)
	config := decode[struct {
		Prefix string `json:"prefix"`
	}](t, a.request(t, http.MethodGet, "/api/v1/api-keys/config", nil), http.StatusOK)
	assert.Equal(t, "pk_", config.Prefix)

	key := decode[struct {
		apiKeyResponse
		Token string `json:"token"`
	}](t, a.request(t, http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "CI deploys"}, a.asAdmin), http.StatusCreated)
	assert.Equal(t, "CI deploys", key.Name)
	path := "/api/v1/api-keys/" + key.ID

	list := decode[[]apiKeyResponse](t, a.request(t, http.MethodGet, "/api/v1/api-keys", nil, a.asAdmin), http.StatusOK)
	assert.Len(t, list, 2)
	assert.Equal(t, key.apiKeyResponse, decode[apiKeyResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK))
	renamed := decode[apiKeyResponse](t, a.request(t, http.MethodPut, path, map[string]any{"name": "Release pipeline"}, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Release pipeline", renamed.Name)

	withKey := func(req *http.Request) { req.Header.Set("X-API-Key", key.Token) }
	assert.Equal(t, http.StatusOK, a.request(t, http.MethodGet, "/api/v1/monitors", nil, withKey).Code)
	w := a.request(t, http.MethodDelete, path, nil, a.asAdmin)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
	assert.Equal(t, http.StatusUnauthorized, a.request(t, http.MethodGet, "/api/v1/monitors", nil, withKey).Code)
}

func TestAPIKeysAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	path := "/api/v1/api-keys/" + decode[[]apiKeyResponse](t, a.request(t, http.MethodGet, "/api/v1/api-keys", nil, a.asAdmin), http.StatusOK)[0].ID
	yesterday := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/api-keys", "{"},
		{"no name", http.MethodPost, "/api/v1/api-keys", map[string]any{"name": ""}},
		{"expired", http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "Old", "expires_at": yesterday}},
		{"no uses allowed", http.MethodPost, "/api/v1/api-keys", map[string]any{"name": "Spent", "max_usage_count": 0}},
		{"update to empty name", http.MethodPut, path, map[string]any{"name": ""}},
		{"update to expired", http.MethodPut, path, map[string]any{"expires_at": yesterday}},
	})

	list := decode[[]apiKeyResponse](t, a.request(t, http.MethodGet, "/api/v1/api-keys", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, "apitest", list[0].Name)
}

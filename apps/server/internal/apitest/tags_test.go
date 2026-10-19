package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tagResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func TestTagsAPI_ManagesTag(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("Production"), a.withAPIKey), http.StatusCreated).ID
	path := "/api/v1/tags/" + id

	got := decode[tagResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, tagResponse{ID: id, Name: "Production", Color: "#3B82F6"}, got)
	list := decode[[]tagResponse](t, a.request(t, http.MethodGet, "/api/v1/tags", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)

	decode[any](t, a.request(t, http.MethodPut, path, tagBody("Staging"), a.asAdmin), http.StatusOK)
	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"color": "#EF4444"}, a.asAdmin), http.StatusOK)
	got = decode[tagResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, tagResponse{ID: id, Name: "Staging", Color: "#EF4444"}, got)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
}

func TestTagsAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/tags", tagBody("Production"), a.asAdmin), http.StatusCreated).ID
	path := "/api/v1/tags/" + id

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/tags", "{"},
		{"no name", http.MethodPost, "/api/v1/tags", tagBody("")},
		{"color not hex", http.MethodPost, "/api/v1/tags", map[string]any{"name": "Staging", "color": "blue"}},
		{"full update with a color not hex", http.MethodPut, path, map[string]any{"name": "Staging", "color": "blue"}},
		{"partial update with a color not hex", http.MethodPatch, path, map[string]any{"color": "blue"}},
		{"negative page", http.MethodGet, "/api/v1/tags?page=-1", nil},
	})
	t.Run("duplicate name", func(t *testing.T) {
		w := a.request(t, http.MethodPost, "/api/v1/tags", tagBody("Production"), a.asAdmin)
		assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})

	got := decode[tagResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, tagResponse{ID: id, Name: "Production", Color: "#3B82F6"}, got)
}

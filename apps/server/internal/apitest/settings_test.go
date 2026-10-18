package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const retentionPath = "/api/v1/settings/key/KEEP_DATA_PERIOD_DAYS"

type settingResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

func TestSettingsAPI_ManagesDataRetentionPeriod(t *testing.T) {
	a := newAppWithAdmin(t)

	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "30", "type": "int"}, a.asAdmin), http.StatusOK)
	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.withAPIKey), http.StatusOK)
	assert.Equal(t, settingResponse{Key: "KEEP_DATA_PERIOD_DAYS", Value: "30", Type: "int"}, got)

	decode[any](t, a.request(t, http.MethodDelete, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin).Code)
}

func TestSettingsAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "30", "type": "int"}, a.asAdmin), http.StatusOK)

	for name, body := range map[string]any{
		"malformed JSON": "{",
		"no type":        map[string]any{"value": "7"},
		"unknown type":   map[string]any{"value": "7", "type": "duration"},
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodPut, retentionPath, body, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}

	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "30", got.Value)
}

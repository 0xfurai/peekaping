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

func TestSettingsAPI_AcceptsADataRetentionPeriodFromOneDayTo100Years(t *testing.T) {
	a := newAppWithAdmin(t)

	for _, days := range []string{"1", "36500"} {
		decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": days, "type": "int"}, a.asAdmin), http.StatusOK)
		got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
		assert.Equal(t, days, got.Value)
	}
}

func TestSettingsAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "30", "type": "int"}, a.asAdmin), http.StatusOK)

	for name, body := range map[string]any{
		"malformed JSON":     "{",
		"no type":            map[string]any{"value": "7"},
		"unknown type":       map[string]any{"value": "7", "type": "duration"},
		"not an int":         map[string]any{"value": "7", "type": "string"},
		"zero days":          map[string]any{"value": "0", "type": "int"},
		"negative days":      map[string]any{"value": "-1", "type": "int"},
		"over 100 years":     map[string]any{"value": "36501", "type": "int"},
		"overflowing days":   map[string]any{"value": "4611686018427387903", "type": "int"},
		"not a number":       map[string]any{"value": "abc", "type": "int"},
		"fractional days":    map[string]any{"value": "1.5", "type": "int"},
		"empty value":        map[string]any{"value": "", "type": "int"},
		"padded with spaces": map[string]any{"value": " 30", "type": "int"},
	} {
		t.Run(name, func(t *testing.T) {
			w := a.request(t, http.MethodPut, retentionPath, body, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}

	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "30", got.Value)
}

package apitest

import (
	"net/http"
	"testing"

	"peekaping/internal/modules/workspace"

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

// The data retention period is a setting of the request's workspace (PP-150, docs/adr/0005 section 5)

func TestSettingsAPI_ANewWorkspaceStartsWith180Days(t *testing.T) {
	a := newAppWithAdmin(t)

	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)

	assert.Equal(t, settingResponse{Key: "KEEP_DATA_PERIOD_DAYS", Value: "180", Type: "int"}, got)
}

func TestSettingsAPI_AMemberReadsTheDataRetentionPeriodAndCannotChangeIt(t *testing.T) {
	a := newAppWithAdmin(t)
	member := a.userIn(t, "member@example.com", a.firstWorkspace(t), workspace.RoleMember)

	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, member), http.StatusOK)
	assert.Equal(t, "180", got.Value)

	put := a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "1", "type": "int"}, member)
	assert.Equal(t, http.StatusForbidden, put.Code, put.Body.String())
	del := a.request(t, http.MethodDelete, retentionPath, nil, member)
	assert.Equal(t, http.StatusForbidden, del.Code, del.Body.String())

	got = decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "180", got.Value, "the period is as it was")
}

func TestSettingsAPI_AnAdminAndAnOwnerChangeTheDataRetentionPeriod(t *testing.T) {
	a := newAppWithAdmin(t)
	admin := a.userIn(t, "admin2@example.com", a.firstWorkspace(t), workspace.RoleAdmin)

	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "20", "type": "int"}, admin), http.StatusOK)
	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "20", got.Value, "an admin's change is the owner's to read")

	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "21", "type": "int"}, a.asAdmin), http.StatusOK)
	got = decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.withAPIKey), http.StatusOK)
	assert.Equal(t, "21", got.Value)
}

func TestSettingsAPI_AnAPIKeyChangesTheDataRetentionPeriodOfItsWorkspace(t *testing.T) {
	a := newAppWithAdmin(t)

	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "9", "type": "int"}, a.withAPIKey), http.StatusOK)

	got := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "9", got.Value)
}

func TestSettingsAPI_TwoWorkspacesHoldDifferentDataRetentionPeriods(t *testing.T) {
	a := newAppWithAdmin(t)
	_, bob := bobsWorkspace(t, a)

	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "3", "type": "int"}, a.asAdmin), http.StatusOK)
	decode[settingResponse](t, a.request(t, http.MethodPut, retentionPath, map[string]any{"value": "300", "type": "int"}, bob), http.StatusOK)

	alices := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	bobs := decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, bob), http.StatusOK)
	assert.Equal(t, "3", alices.Value)
	assert.Equal(t, "300", bobs.Value)

	decode[any](t, a.request(t, http.MethodDelete, retentionPath, nil, bob), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, retentionPath, nil, bob).Code)
	alices = decode[settingResponse](t, a.request(t, http.MethodGet, retentionPath, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "3", alices.Value, "Bob's delete left Alice's period alone")
}

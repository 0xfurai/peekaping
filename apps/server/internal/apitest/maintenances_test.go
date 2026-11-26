package apitest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type maintenanceResponse struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Strategy   string   `json:"strategy"`
	Active     bool     `json:"active"`
	MonitorIDs []string `json:"monitor_ids"`
	StartTime  string   `json:"start_time"`
	EndTime    string   `json:"end_time"`
}

func TestMaintenancesAPI_ManagesMaintenanceWindow(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	body := weeklyMaintenanceBody("Database upgrade", "09:30", "10:30")
	body["monitor_ids"] = []string{monitorID}
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.withAPIKey), http.StatusCreated).ID
	path := "/api/v1/maintenances/" + id

	got := decode[maintenanceResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, maintenanceResponse{
		ID: id, Title: "Database upgrade", Strategy: "recurring-weekday", Active: true, MonitorIDs: []string{monitorID},
		StartTime: "09:30", EndTime: "10:30",
	}, got)
	list := decode[[]maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)

	decode[any](t, a.request(t, http.MethodPut, path, weeklyMaintenanceBody("Database upgrade, part 2", "08:00", "09:30"), a.asAdmin), http.StatusOK)
	got = decode[maintenanceResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Database upgrade, part 2", got.Title)
	assert.Equal(t, [2]string{"08:00", "09:30"}, [2]string{got.StartTime, got.EndTime})

	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"title": "Database upgrade, part 3", "start_time": "09:30", "end_time": "11:00"}, a.asAdmin), http.StatusOK)
	got = decode[maintenanceResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Database upgrade, part 3", got.Title)
	assert.Equal(t, [2]string{"09:30", "11:00"}, [2]string{got.StartTime, got.EndTime})

	assert.False(t, decode[maintenanceResponse](t, a.request(t, http.MethodPatch, path+"/pause", nil, a.asAdmin), http.StatusOK).Active)
	assert.True(t, decode[maintenanceResponse](t, a.request(t, http.MethodPatch, path+"/resume", nil, a.asAdmin), http.StatusOK).Active)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
}

func TestMaintenancesAPI_CreatesInactiveMaintenanceWindow(t *testing.T) {
	a := newAppWithAdmin(t)
	body := with(maintenanceBody("Inactive maintenance"), "active", false)

	created := decode[maintenanceResponse](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.asAdmin), http.StatusCreated)
	assert.False(t, created.Active)

	got := decode[maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestMaintenancesAPI_DefaultsMaintenanceToActiveWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	body := maintenanceBody("Default maintenance")
	delete(body, "active")

	created := decode[maintenanceResponse](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.asAdmin), http.StatusCreated)
	assert.True(t, created.Active)

	got := decode[maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.True(t, got.Active)
}

func TestMaintenancesAPI_UpdateFullDoesNotEnableInactiveMaintenanceWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	createBody := maintenanceBody("Inactive maintenance")
	createBody["active"] = false
	created := decode[maintenanceResponse](t, a.request(t, http.MethodPost, "/api/v1/maintenances", createBody, a.asAdmin), http.StatusCreated)
	require.False(t, created.Active)

	updateBody := maintenanceBody("Inactive maintenance")
	delete(updateBody, "active")
	updated := decode[maintenanceResponse](t, a.request(t, http.MethodPut, "/api/v1/maintenances/"+created.ID, updateBody, a.asAdmin), http.StatusOK)
	assert.False(t, updated.Active)

	got := decode[maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestMaintenancesAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", maintenanceBody("Database upgrade"), a.asAdmin), http.StatusCreated).ID

	upgrade := maintenanceBody("Database upgrade")
	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/maintenances", "{"},
		{"no title", http.MethodPost, "/api/v1/maintenances", with(upgrade, "title", "")},
		{"no strategy", http.MethodPost, "/api/v1/maintenances", with(upgrade, "strategy", "")},
		{"start not a date and time", http.MethodPost, "/api/v1/maintenances", with(upgrade, "start_date_time", "tomorrow")},
		{"weekday out of range", http.MethodPost, "/api/v1/maintenances", with(upgrade, "weekdays", []int{7})},
		{"negative duration", http.MethodPost, "/api/v1/maintenances", with(upgrade, "duration", -5)},
		{"start time out of range", http.MethodPost, "/api/v1/maintenances", with(upgrade, "start_time", "25:99")},
		{"end time not a time", http.MethodPost, "/api/v1/maintenances", with(upgrade, "end_time", "noon")},
		{"full update without a title", http.MethodPut, "/api/v1/maintenances/" + id, with(upgrade, "title", "")},
		{"full update with a start time out of range", http.MethodPut, "/api/v1/maintenances/" + id, with(upgrade, "start_time", "25:99")},
		{"partial update with malformed JSON", http.MethodPatch, "/api/v1/maintenances/" + id, "{"},
		{"partial update with a start time out of range", http.MethodPatch, "/api/v1/maintenances/" + id, map[string]any{"start_time": "25:99"}},
		{"partial update with a weekday out of range", http.MethodPatch, "/api/v1/maintenances/" + id, map[string]any{"weekdays": []int{7}}},
		{"negative page", http.MethodGet, "/api/v1/maintenances?page=-1", nil},
	})

	list := decode[[]maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, "Database upgrade", list[0].Title)
	assert.Empty(t, list[0].StartTime)
}

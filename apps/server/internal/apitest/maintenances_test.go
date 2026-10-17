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
}

func TestMaintenancesAPI_ManagesMaintenanceWindow(t *testing.T) {
	a := newAppWithAdmin(t)
	monitorID := a.createMonitor(t, "Checkout", "checkout-push-token")
	body := maintenanceBody("Database upgrade")
	body["monitor_ids"] = []string{monitorID}
	id := decode[created](t, a.request(t, http.MethodPost, "/api/v1/maintenances", body, a.withAPIKey), http.StatusCreated).ID
	path := "/api/v1/maintenances/" + id

	got := decode[maintenanceResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, maintenanceResponse{ID: id, Title: "Database upgrade", Strategy: "manual", Active: true, MonitorIDs: []string{monitorID}}, got)
	list := decode[[]maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)

	decode[any](t, a.request(t, http.MethodPut, path, maintenanceBody("Database upgrade, part 2"), a.asAdmin), http.StatusOK)
	decode[any](t, a.request(t, http.MethodPatch, path, map[string]any{"title": "Database upgrade, part 3"}, a.asAdmin), http.StatusOK)
	got = decode[maintenanceResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Database upgrade, part 3", got.Title)

	assert.False(t, decode[maintenanceResponse](t, a.request(t, http.MethodPatch, path+"/pause", nil, a.asAdmin), http.StatusOK).Active)
	assert.True(t, decode[maintenanceResponse](t, a.request(t, http.MethodPatch, path+"/resume", nil, a.asAdmin), http.StatusOK).Active)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
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
		{"full update without a title", http.MethodPut, "/api/v1/maintenances/" + id, with(upgrade, "title", "")},
		{"partial update with malformed JSON", http.MethodPatch, "/api/v1/maintenances/" + id, "{"},
		{"negative page", http.MethodGet, "/api/v1/maintenances?page=-1", nil},
	})

	list := decode[[]maintenanceResponse](t, a.request(t, http.MethodGet, "/api/v1/maintenances", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, "Database upgrade", list[0].Title)
}

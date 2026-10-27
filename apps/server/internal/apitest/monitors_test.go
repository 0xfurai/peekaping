package apitest

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type monitorResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Interval int    `json:"interval"`
	Active   bool   `json:"active"`
}

func TestMonitorsAPI_ManagesMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	path := "/api/v1/monitors/" + id

	got := decode[monitorResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, monitorResponse{ID: id, Name: "Checkout", Type: "push", Interval: 60, Active: true}, got)

	list := decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors?q=check", nil, a.withAPIKey), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, id, list[0].ID)
	batch := decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/batch?ids="+id, nil, a.asAdmin), http.StatusOK)
	require.Len(t, batch, 1)
	assert.Equal(t, id, batch[0].ID)

	decode[monitorResponse](t, a.request(t, http.MethodPut, path, pushMonitorBody("Checkout API", "checkout-push-token"), a.asAdmin), http.StatusOK)
	decode[monitorResponse](t, a.request(t, http.MethodPatch, path, map[string]any{"interval": 120}, a.asAdmin), http.StatusOK)
	got = decode[monitorResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, "Checkout API", got.Name)
	assert.Equal(t, 120, got.Interval)

	heartbeats := decode[[]map[string]any](t, a.request(t, http.MethodGet, path+"/heartbeats", nil, a.asAdmin), http.StatusOK)
	assert.Empty(t, heartbeats)
	decode[map[string]any](t, a.request(t, http.MethodGet, path+"/stats/uptime", nil, a.asAdmin), http.StatusOK)
	since := url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
	decode[map[string]any](t, a.request(t, http.MethodGet, path+"/stats/points?since="+since, nil, a.asAdmin), http.StatusOK)
	assert.Nil(t, decode[any](t, a.request(t, http.MethodGet, path+"/tls", nil, a.asAdmin), http.StatusOK))
	decode[any](t, a.request(t, http.MethodPost, path+"/reset", nil, a.asAdmin), http.StatusOK)

	decode[any](t, a.request(t, http.MethodDelete, path, nil, a.asAdmin), http.StatusOK)
	assert.Equal(t, http.StatusNotFound, a.request(t, http.MethodGet, path, nil, a.asAdmin).Code)
}

func TestMonitorsAPI_CreatesPausedMonitor(t *testing.T) {
	a := newAppWithAdmin(t)
	body := with(pushMonitorBody("Paused monitor", "paused-monitor-token"), "active", false)

	created := decode[monitorResponse](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)
	assert.False(t, created.Active)

	got := decode[monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestMonitorsAPI_DefaultsMonitorToActiveWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	body := pushMonitorBody("Default monitor", "default-monitor-token")
	delete(body, "active")

	created := decode[monitorResponse](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)
	assert.True(t, created.Active)

	got := decode[monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.True(t, got.Active)
}

func TestMonitorsAPI_UpdateFullDoesNotEnablePausedMonitorWhenActiveOmitted(t *testing.T) {
	a := newAppWithAdmin(t)
	createBody := pushMonitorBody("Paused monitor", "paused-monitor-token")
	createBody["active"] = false
	created := decode[monitorResponse](t, a.request(t, http.MethodPost, "/api/v1/monitors", createBody, a.asAdmin), http.StatusCreated)
	require.False(t, created.Active)

	updateBody := pushMonitorBody("Paused monitor", "paused-monitor-token")
	delete(updateBody, "active")
	updated := decode[monitorResponse](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+created.ID, updateBody, a.asAdmin), http.StatusOK)
	assert.False(t, updated.Active)

	got := decode[monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+created.ID, nil, a.asAdmin), http.StatusOK)
	assert.False(t, got.Active)
}

func TestMonitorsAPI_RejectsInvalidInput(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	path := "/api/v1/monitors/" + id

	monitor := pushMonitorBody("Search", "search-push-token")
	shortName := with(monitor, "name", "ab")

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"malformed JSON", http.MethodPost, "/api/v1/monitors", "{"},
		{"name too short", http.MethodPost, "/api/v1/monitors", shortName},
		{"config fails its monitor type", http.MethodPost, "/api/v1/monitors", with(monitor, "config", `{"pushToken":5}`)},
		{"unknown monitor type", http.MethodPost, "/api/v1/monitors", with(monitor, "type", "carrier-pigeon")},
		{"full update with a name too short", http.MethodPut, path, shortName},
		{"partial update with a bad config", http.MethodPatch, path, map[string]any{"config": "{}"}},
		{"negative page", http.MethodGet, "/api/v1/monitors?page=-1", nil},
		{"batch without ids", http.MethodGet, "/api/v1/monitors/batch", nil},
		{"stat points without since", http.MethodGet, path + "/stats/points", nil},
		{"heartbeats with a bad limit", http.MethodGet, path + "/heartbeats?limit=0", nil},
	})

	list := decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
	assert.Equal(t, "Checkout", list[0].Name)
}

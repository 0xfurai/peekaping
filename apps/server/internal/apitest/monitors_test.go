package apitest

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
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

// A timeout is at most 0.8 x the check interval, so a check ends before the next one is due.
func TestMonitorsAPI_RejectsATimeoutAboveEightyPercentOfTheInterval(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	path := "/api/v1/monitors/" + id

	tooLong := with(with(pushMonitorBody("Search", "search-push-token"), "interval", 60), "timeout", 49)
	reply := a.request(t, http.MethodPost, "/api/v1/monitors", tooLong, a.asAdmin)
	assert.Equal(t, http.StatusBadRequest, reply.Code, reply.Body.String())
	assert.Contains(t, reply.Body.String(), "timeout", "the error names the timeout")

	a.expectStatus(t, http.StatusBadRequest, []call{
		{"full update with a timeout of 49 s on a 60 s interval", http.MethodPut, path, with(with(pushMonitorBody("Checkout", "checkout-push-token"), "interval", 60), "timeout", 49)},
		{"timeout longer than the interval", http.MethodPost, "/api/v1/monitors", with(with(pushMonitorBody("Search", "search-push-token"), "interval", 60), "timeout", 90)},
	})

	list := decode[[]monitorResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors", nil, a.asAdmin), http.StatusOK)
	require.Len(t, list, 1)
}

// monitorTimings are the check interval, timeout and retry interval a monitor is stored with.
type monitorTimings struct {
	Interval      int `json:"interval"`
	Timeout       int `json:"timeout"`
	RetryInterval int `json:"retry_interval"`
}

func (a *app) monitorTimings(t *testing.T, id string) monitorTimings {
	t.Helper()
	return decode[monitorTimings](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+id, nil, a.asAdmin), http.StatusOK)
}

// createMonitorWithTimings creates a push monitor with the given check interval and timeout.
func (a *app) createMonitorWithTimings(t *testing.T, interval, timeout int) string {
	t.Helper()
	body := with(with(pushMonitorBody("Timed", "timed-push-token"), "interval", interval), "timeout", timeout)
	return decode[created](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated).ID
}

// The timeout rule holds for a partial update too, checked against the values the monitor would
// have after it.
func TestMonitorsAPI_PatchRejectsATimeoutAboveEightyPercentOfTheInterval(t *testing.T) {
	for _, c := range []struct {
		name              string
		patch             map[string]any
		interval, timeout int
	}{
		{"a shorter check interval", map[string]any{"interval": 20}, 60, 48},
		{"a longer timeout", map[string]any{"timeout": 49}, 60, 16},
		{"both together", map[string]any{"interval": 30, "timeout": 25}, 60, 48},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := newAppWithAdmin(t)
			id := a.createMonitorWithTimings(t, c.interval, c.timeout)

			reply := a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, c.patch, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, reply.Code, reply.Body.String())
			assert.Contains(t, reply.Body.String(), "timeout", "the error names the timeout")

			assert.Equal(t, monitorTimings{Interval: c.interval, Timeout: c.timeout, RetryInterval: 60}, a.monitorTimings(t, id), "nothing is stored")
		})
	}
}

func TestMonitorsAPI_PatchRejectsTimingsBelowTheirMinimum(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitorWithTimings(t, 60, 48)
	path := "/api/v1/monitors/" + id

	// A shorter check interval also breaks the timeout rule here, so each error must name the minimum.
	// An interval names it as INTERVAL_TOO_SHORT does (PP-159).
	for _, c := range []struct {
		call
		want string
	}{
		{call{"check interval of 19 s", http.MethodPatch, path, map[string]any{"interval": 19}}, "below the minimum of 20 seconds"},
		{call{"check interval of 0", http.MethodPatch, path, map[string]any{"interval": 0}}, "below the minimum of 20 seconds"},
		{call{"timeout of 15 s", http.MethodPatch, path, map[string]any{"timeout": 15}}, "'min' tag"},
		{call{"timeout of 0", http.MethodPatch, path, map[string]any{"timeout": 0}}, "'min' tag"},
		{call{"retry interval of 19 s", http.MethodPatch, path, map[string]any{"retry_interval": 19}}, "below the minimum of 20 seconds"},
		{call{"retry interval of 0", http.MethodPatch, path, map[string]any{"retry_interval": 0}}, "below the minimum of 20 seconds"},
	} {
		t.Run(c.name, func(t *testing.T) {
			reply := a.request(t, c.method, c.path, c.body, a.asAdmin)
			assert.Equal(t, http.StatusBadRequest, reply.Code, reply.Body.String())
			assert.Contains(t, reply.Body.String(), c.want, "the error names the minimum")
		})
	}

	assert.Equal(t, monitorTimings{Interval: 60, Timeout: 48, RetryInterval: 60}, a.monitorTimings(t, id), "nothing is stored")
}

func TestMonitorsAPI_PatchStoresValidTimings(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitorWithTimings(t, 60, 48)
	path := "/api/v1/monitors/" + id

	reply := a.request(t, http.MethodPatch, path, map[string]any{"interval": 120}, a.asAdmin)
	require.Equal(t, http.StatusOK, reply.Code, reply.Body.String())
	assert.Equal(t, monitorTimings{Interval: 120, Timeout: 48, RetryInterval: 60}, a.monitorTimings(t, id))

	reply = a.request(t, http.MethodPatch, path, map[string]any{"interval": 20, "timeout": 16, "retry_interval": 20}, a.asAdmin)
	require.Equal(t, http.StatusOK, reply.Code, reply.Body.String())
	assert.Equal(t, monitorTimings{Interval: 20, Timeout: 16, RetryInterval: 20}, a.monitorTimings(t, id))
}

// A monitor stored with a timeout above 0.8 x its check interval (made by an older version) can
// still be paused, resumed and renamed: a partial update that sends neither is not held to the rule.
func TestMonitorsAPI_PatchLeavesAMonitorThatBreaksTheTimeoutRuleAlone(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.createMonitorWithTimings(t, 60, 48)
	require.NoError(t, a.container.Invoke(func(db *bun.DB) error {
		_, err := db.ExecContext(context.Background(), "UPDATE monitors SET interval = 20, timeout = 48 WHERE id = ?", id)
		return err
	}))
	require.Equal(t, monitorTimings{Interval: 20, Timeout: 48, RetryInterval: 60}, a.monitorTimings(t, id))
	path := "/api/v1/monitors/" + id

	a.expectStatus(t, http.StatusOK, []call{
		{"pause", http.MethodPatch, path, map[string]any{"active": false}},
		{"resume", http.MethodPatch, path, map[string]any{"active": true}},
		{"rename", http.MethodPatch, path, map[string]any{"name": "Renamed"}},
	})
	assert.Equal(t, monitorTimings{Interval: 20, Timeout: 48, RetryInterval: 60}, a.monitorTimings(t, id))
}

func TestMonitorsAPI_AcceptsATimeoutOfAtMostEightyPercentOfTheInterval(t *testing.T) {
	a := newAppWithAdmin(t)
	for i, c := range []struct {
		name              string
		interval, timeout int
	}{
		{"80 percent of 60", 60, 48},
		{"80 percent of the shortest interval", 20, 16},
	} {
		t.Run(c.name, func(t *testing.T) {
			token := fmt.Sprintf("accepted-token-%d", i)
			body := with(with(pushMonitorBody("Accepted", token), "interval", c.interval), "timeout", c.timeout)
			decode[monitorResponse](t, a.request(t, http.MethodPost, "/api/v1/monitors", body, a.asAdmin), http.StatusCreated)
		})
	}
}

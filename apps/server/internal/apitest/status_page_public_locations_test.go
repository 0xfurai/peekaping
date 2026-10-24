package apitest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordLocationCheck stores a check as the ingester does for a monitor's location: the heartbeat,
// the stats it counts in and the state of the probe's row.
func (a *app) recordLocationCheck(t *testing.T, monitorID, probeID string, status shared.MonitorStatus, ping int, at time.Time) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service, statService stats.Service, monitors monitor.MonitorRepository) error {
		ctx := context.Background()
		if _, err := heartbeats.Create(ctx, &heartbeat.CreateUpdateDto{MonitorID: monitorID, ProbeID: probeID, Status: status, Ping: ping, Time: at, EndTime: at}); err != nil {
			return err
		}
		if err := statService.AggregateHeartbeat(ctx, &stats.HeartbeatPayload{MonitorID: monitorID, ProbeID: probeID, Status: int(status), Ping: ping, Time: at.Unix()}); err != nil {
			return err
		}
		state := shared.LocationState{ProbeID: probeID, Status: &status, LastPing: &ping, LastHeartbeatAt: &at}
		return monitors.RecordLocation(ctx, monitorID, state, func(shared.LocationJudgement) *shared.MonitorStatus { return nil })
	}))
}

type publicHeartbeat struct {
	Status int `json:"status"`
	Ping   int `json:"ping"`
}

type publicLocation struct {
	Name       string            `json:"name"`
	Status     int               `json:"status"`
	Uptime24h  float64           `json:"uptime_24h"`
	Heartbeats []publicHeartbeat `json:"heartbeats"`
}

type publicMonitorWithLocations struct {
	Name       string            `json:"name"`
	Status     *int              `json:"status"`
	Uptime24h  float64           `json:"uptime_24h"`
	Heartbeats []publicHeartbeat `json:"heartbeats"`
	Locations  []publicLocation  `json:"locations"`
}

// checkoutOnTwoProbes is a monitor stored as Down that runs on local and a probe named Frankfurt.
// Local checked Up twice; Frankfurt checked Up, then Down.
func (a *app) checkoutOnTwoProbes(t *testing.T) string {
	t.Helper()
	frankfurt := a.probeThatRuns(t, "Frankfurt", "http")
	id := decode[created](t, a.request(t, http.MethodPost, monitorsPath, onProbes(httpMonitorBody("Checkout", ""), "local", frankfurt.ID), a.asAdmin), http.StatusCreated).ID
	at := time.Now().UTC().Add(-time.Hour)
	a.recordLocationCheck(t, id, "local", shared.MonitorStatusUp, 100, at)
	a.recordLocationCheck(t, id, frankfurt.ID, shared.MonitorStatusUp, 300, at.Add(time.Minute))
	a.recordLocationCheck(t, id, "local", shared.MonitorStatusUp, 110, at.Add(2*time.Minute))
	a.recordLocationCheck(t, id, frankfurt.ID, shared.MonitorStatusDown, 0, at.Add(3*time.Minute))
	a.storeStatus(t, id, shared.MonitorStatusDown)
	return id
}

var publicMonitorPaths = []string{"/api/v1/status-pages/slug/shop/monitors", "/api/v1/status-pages/slug/shop/monitors/homepage"}

func TestPublicStatusPage_ShowsTheMonitorsOwnStatusAndNoLocationsByDefault(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.checkoutOnTwoProbes(t)
	a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{id}})

	for _, path := range publicMonitorPaths {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			monitors := decode[[]publicMonitorWithLocations](t, w, http.StatusOK)
			require.Len(t, monitors, 1)
			if assert.NotNil(t, monitors[0].Status, "the monitor's own status") {
				assert.Equal(t, 0, *monitors[0].Status)
			}
			assert.Nil(t, monitors[0].Locations)
			assert.NotContains(t, w.Body.String(), `"locations"`)
			assert.NotContains(t, w.Body.String(), "Frankfurt")
		})
	}
}

func TestPublicStatusPage_ListsEachLocationWhenTheSettingIsOn(t *testing.T) {
	a := newAppWithAdmin(t)
	id := a.checkoutOnTwoProbes(t)
	single := a.createHTTPMonitor(t, "Search", "")
	a.recordLocationCheck(t, single, "local", shared.MonitorStatusUp, 50, time.Now().UTC().Add(-time.Minute))
	a.storeStatus(t, single, shared.MonitorStatusUp)
	pageID := a.createStatusPage(t, statusPage{slug: "shop", published: true, monitors: []string{id, single}})
	decode[any](t, a.request(t, http.MethodPatch, "/api/v1/status-pages/"+pageID, map[string]any{"show_locations": true}, a.asAdmin), http.StatusOK)

	for _, path := range publicMonitorPaths {
		t.Run(path, func(t *testing.T) {
			w := a.request(t, http.MethodGet, path, nil)
			monitors := decode[[]publicMonitorWithLocations](t, w, http.StatusOK)
			require.Len(t, monitors, 2)
			byName := map[string]publicMonitorWithLocations{}
			for _, m := range monitors {
				byName[m.Name] = m
			}

			checkout := byName["Checkout"]
			if assert.NotNil(t, checkout.Status) {
				assert.Equal(t, 0, *checkout.Status)
			}
			// Local first, then the others by name. The homepage payload carries each bar's latest heartbeat only.
			if path == publicMonitorPaths[0] {
				assert.Equal(t, []publicLocation{
					{Name: "Local", Status: 1, Uptime24h: 100, Heartbeats: []publicHeartbeat{{Status: 1, Ping: 100}, {Status: 1, Ping: 110}}},
					{Name: "Frankfurt", Status: 0, Uptime24h: 50, Heartbeats: []publicHeartbeat{{Status: 1, Ping: 300}, {Status: 0, Ping: 0}}},
				}, checkout.Locations)
			} else {
				assert.Equal(t, []publicLocation{
					{Name: "Local", Status: 1, Uptime24h: 100, Heartbeats: []publicHeartbeat{{Status: 1, Ping: 110}}},
					{Name: "Frankfurt", Status: 0, Uptime24h: 50, Heartbeats: []publicHeartbeat{{Status: 0, Ping: 0}}},
				}, checkout.Locations)
			}

			search := byName["Search"]
			if assert.NotNil(t, search.Status) {
				assert.Equal(t, 1, *search.Status)
			}
			assert.Nil(t, search.Locations, "a monitor on one probe has no locations to show")
			assert.NotContains(t, w.Body.String(), "probe_id")
			assert.NotContains(t, w.Body.String(), `"online"`)
		})
	}
}

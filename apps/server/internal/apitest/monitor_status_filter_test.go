package apitest

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type monitorStatusResponse struct {
	ID        string    `json:"id"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// newAppWithMonitorListener is an API server that also stores a Monitor's status when a
// Heartbeat changes it, as cmd/api does.
func newAppWithMonitorListener(t *testing.T) *app {
	t.Helper()
	a := newAppWithAdmin(t)
	require.NoError(t, a.container.Invoke(func(listener *monitor.MonitorEventListener, bus events.EventBus) {
		listener.Subscribe(bus)
	}))
	return a
}

// reportStatus records a Heartbeat with the status and announces the change the way the ingester does,
// then waits for the Monitor listener to store it.
func (a *app) reportStatus(t *testing.T, monitorID string, status shared.MonitorStatus) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service, bus events.EventBus) error {
		created, err := heartbeats.Create(context.Background(), &heartbeat.CreateUpdateDto{MonitorID: monitorID, Status: status})
		if err == nil {
			bus.Publish(events.Event{Type: events.MonitorStatusChanged, Payload: created})
		}
		return err
	}))
	require.Eventually(t, func() bool {
		return a.getMonitor(t, monitorID).Status == int(status)
	}, 5*time.Second, 20*time.Millisecond)
}

// getMonitor reads a Monitor in the shape a PUT returns it, so created_at compares exactly.
func (a *app) getMonitor(t *testing.T, monitorID string) monitorStatusResponse {
	t.Helper()
	monitors := decode[[]monitorStatusResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/batch?ids="+monitorID, nil, a.asAdmin), http.StatusOK)
	require.Len(t, monitors, 1)
	return monitors[0]
}

// monitorIDsWithStatus returns the IDs the monitor list shows under a status filter.
func (a *app) monitorIDsWithStatus(t *testing.T, status shared.MonitorStatus) []string {
	t.Helper()
	path := "/api/v1/monitors?status=" + strconv.Itoa(int(status))
	var ids []string
	for _, m := range decode[[]monitorStatusResponse](t, a.request(t, http.MethodGet, path, nil, a.asAdmin), http.StatusOK) {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestMonitorStatusFilter_DownMonitorEditedWithPutStaysUnderDown(t *testing.T) {
	a := newAppWithMonitorListener(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.reportStatus(t, id, shared.MonitorStatusDown)

	w := a.request(t, http.MethodPut, "/api/v1/monitors/"+id, pushMonitorBody("Checkout API", "checkout-push-token"), a.asAdmin)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{id}, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusUp))
}

func TestMonitorStatusFilter_DownMonitorChangedWithPatchStaysUnderDown(t *testing.T) {
	a := newAppWithMonitorListener(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.reportStatus(t, id, shared.MonitorStatusDown)

	a.pauseMonitor(t, id)
	assert.Equal(t, []string{id}, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))

	w := a.request(t, http.MethodPatch, "/api/v1/monitors/"+id, map[string]any{"active": true, "name": "Checkout API"}, a.asAdmin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, []string{id}, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusUp))
}

func TestMonitorStatusFilter_NewMonitorIsPendingUntilItsFirstHeartbeat(t *testing.T) {
	a := newAppWithMonitorListener(t)

	createdMonitor := decode[monitorStatusResponse](t, a.request(t, http.MethodPost, "/api/v1/monitors", pushMonitorBody("Checkout", "checkout-push-token"), a.asAdmin), http.StatusCreated)

	assert.Equal(t, int(shared.MonitorStatusPending), createdMonitor.Status)
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusUp))
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))

	a.reportStatus(t, createdMonitor.ID, shared.MonitorStatusUp)
	assert.Equal(t, []string{createdMonitor.ID}, a.monitorIDsWithStatus(t, shared.MonitorStatusUp))
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))
}

func TestMonitorStatusFilter_PutResponseShowsTheStoredMonitor(t *testing.T) {
	a := newAppWithMonitorListener(t)
	id := a.createMonitor(t, "Checkout", "checkout-push-token")
	a.reportStatus(t, id, shared.MonitorStatusMaintenance)
	before := a.getMonitor(t, id)
	require.False(t, before.CreatedAt.IsZero())

	got := decode[monitorStatusResponse](t, a.request(t, http.MethodPut, "/api/v1/monitors/"+id, pushMonitorBody("Checkout API", "checkout-push-token"), a.asAdmin), http.StatusOK)

	assert.Equal(t, int(shared.MonitorStatusMaintenance), got.Status)
	assert.True(t, got.CreatedAt.Equal(before.CreatedAt), "created_at %v should stay %v", got.CreatedAt, before.CreatedAt)
}

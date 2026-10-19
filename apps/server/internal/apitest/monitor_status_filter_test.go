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

func (a *app) getMonitor(t *testing.T, monitorID string) monitorStatusResponse {
	t.Helper()
	return decode[monitorStatusResponse](t, a.request(t, http.MethodGet, "/api/v1/monitors/"+monitorID, nil, a.asAdmin), http.StatusOK)
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

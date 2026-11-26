package apitest

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type storedMonitor struct {
	ID        string    `json:"id"`
	Status    int       `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// storeStatus sets a Monitor's stored status the way the listener does: through the service, with no event.
func (a *app) storeStatus(t *testing.T, monitorID string, status shared.MonitorStatus) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		_, err := monitors.UpdatePartialAnyWorkspace(context.Background(), monitorID, &monitor.PartialUpdateDto{Status: &status}, true)
		return err
	}))
}

// recordHeartbeat saves a Heartbeat without announcing a status change, so the Monitor's stored status stays as it is.
func (a *app) recordHeartbeat(t *testing.T, monitorID string, status shared.MonitorStatus) {
	t.Helper()
	require.NoError(t, a.container.Invoke(func(heartbeats heartbeat.Service) error {
		_, err := heartbeats.Create(context.Background(), &heartbeat.CreateUpdateDto{MonitorID: monitorID, Status: status})
		return err
	}))
}

// repairStoredStatuses runs the sweep that cmd/api starts, and returns how many Monitors it fixed.
func (a *app) repairStoredStatuses(t *testing.T) int {
	t.Helper()
	var repaired int
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		var err error
		repaired, err = monitors.RepairStoredStatusesAnyWorkspace(context.Background())
		return err
	}))
	return repaired
}

// storedMonitors lists every Monitor by ID, as the monitor list returns them.
func (a *app) storedMonitors(t *testing.T) map[string]storedMonitor {
	t.Helper()
	listed := decode[[]storedMonitor](t, a.request(t, http.MethodGet, "/api/v1/monitors?limit=1000", nil, a.asAdmin), http.StatusOK)
	byID := make(map[string]storedMonitor, len(listed))
	for _, m := range listed {
		byID[m.ID] = m
	}
	return byID
}

func TestRepairStoredStatuses_StoredStatusFollowsTheLatestHeartbeat(t *testing.T) {
	a := newAppWithAdmin(t)

	// A Down Monitor that is stored as Up, so the Up filter lists it
	checkout := a.createMonitor(t, "Checkout", "checkout-token")
	a.storeStatus(t, checkout, shared.MonitorStatusUp)
	a.recordHeartbeat(t, checkout, shared.MonitorStatusDown)

	// Still stored as Pending, as a new Monitor is, after its first Heartbeat came in Up
	billing := a.createMonitor(t, "Billing", "billing-token")
	a.recordHeartbeat(t, billing, shared.MonitorStatusUp)

	// Its stored status is the oldest Heartbeat's, and its newest is Maintenance
	search := a.createMonitor(t, "Search", "search-token")
	a.recordHeartbeat(t, search, shared.MonitorStatusDown)
	a.recordHeartbeat(t, search, shared.MonitorStatusUp)
	a.recordHeartbeat(t, search, shared.MonitorStatusMaintenance)
	a.storeStatus(t, search, shared.MonitorStatusDown)

	// Paused Monitors are repaired too
	nightly := a.createMonitor(t, "Nightly", "nightly-token")
	a.pauseMonitor(t, nightly)
	a.storeStatus(t, nightly, shared.MonitorStatusUp)
	a.recordHeartbeat(t, nightly, shared.MonitorStatusDown)

	// Already right
	healthy := a.createMonitor(t, "Healthy", "healthy-token")
	a.storeStatus(t, healthy, shared.MonitorStatusUp)
	a.recordHeartbeat(t, healthy, shared.MonitorStatusUp)

	// No Heartbeat to go by, so its stored status stays, though Pending is what a new Monitor has
	silent := a.createMonitor(t, "Silent", "silent-token")
	a.storeStatus(t, silent, shared.MonitorStatusDown)

	before := a.storedMonitors(t)

	repaired := a.repairStoredStatuses(t)

	assert.Equal(t, 4, repaired)
	after := a.storedMonitors(t)
	statuses := make(map[string]int, len(after))
	for id, m := range after {
		statuses[id] = m.Status
	}
	assert.Equal(t, map[string]int{
		checkout: int(shared.MonitorStatusDown),
		billing:  int(shared.MonitorStatusUp),
		search:   int(shared.MonitorStatusMaintenance),
		nightly:  int(shared.MonitorStatusDown),
		healthy:  int(shared.MonitorStatusUp),
		silent:   int(shared.MonitorStatusDown),
	}, statuses)
	assert.ElementsMatch(t, []string{checkout, nightly, silent}, a.monitorIDsWithStatus(t, shared.MonitorStatusDown))
	assert.ElementsMatch(t, []string{billing, healthy}, a.monitorIDsWithStatus(t, shared.MonitorStatusUp))
	assert.Equal(t, []string{search}, a.monitorIDsWithStatus(t, shared.MonitorStatusMaintenance))
	assert.Empty(t, a.monitorIDsWithStatus(t, shared.MonitorStatusPending))
	for _, untouched := range []string{healthy, silent} {
		assert.True(t, after[untouched].UpdatedAt.Equal(before[untouched].UpdatedAt), "%s was saved again: updated_at %v was %v", untouched, after[untouched].UpdatedAt, before[untouched].UpdatedAt)
	}

	assert.Zero(t, a.repairStoredStatuses(t), "a second sweep has nothing left to fix")
}

func TestRepairStoredStatuses_WalksEveryPageOfMonitors(t *testing.T) {
	parallel(t)
	// A full last page is followed by an empty one, so the sweep has to stop on that too
	for _, count := range []int{monitor.StatusRepairPageSize, monitor.StatusRepairPageSize + 1} {
		t.Run(strconv.Itoa(count)+" monitors", func(t *testing.T) {
			a := newAppWithAdmin(t)
			ids := make([]string, count)
			for i := range ids {
				ids[i] = a.createStoredMonitor(t, fmt.Sprintf("Monitor %03d", i))
				a.recordHeartbeat(t, ids[i], shared.MonitorStatusUp)
			}

			repaired := a.repairStoredStatuses(t)

			assert.Equal(t, count, repaired)
			stored := a.storedMonitors(t)
			require.Len(t, stored, count)
			for _, id := range ids {
				assert.Equal(t, int(shared.MonitorStatusUp), stored[id].Status, "monitor %s", id)
			}
		})
	}
}

// createStoredMonitor creates a Monitor through the service, which is quicker than the API for a crowd of them.
func (a *app) createStoredMonitor(t *testing.T, name string) string {
	t.Helper()
	var id string
	workspaceID := a.firstWorkspace(t)
	require.NoError(t, a.container.Invoke(func(monitors monitor.Service) error {
		created, err := monitors.Create(context.Background(), workspaceID, &monitor.CreateUpdateDto{
			Type: "push", Name: name, Interval: 60, Timeout: 16, RetryInterval: 60, Active: true,
			PushToken: "token-" + name, Config: fmt.Sprintf(`{"pushToken":"token-%s"}`, name),
		})
		if err == nil {
			id = created.ID
		}
		return err
	}))
	return id
}

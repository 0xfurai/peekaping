package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"peekaping/internal/modules/workspace/workspacetest"
	"testing"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// localLocationOnline creates a monitor of the local probe in the container's install, with the
// local probe offline (it is, until an instance connects), and reports whether the monitor's
// location on the local probe is online
func localLocationOnline(t *testing.T, container *dig.Container) bool {
	t.Helper()
	var online bool
	require.NoError(t, container.Invoke(func(monitors monitor.Service, probes probe.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))
		ctx := context.Background()
		created, err := monitors.Create(ctx, workspacetest.ID, &monitor.CreateUpdateDto{Type: "http", Name: "Home", Active: true, Config: "{}"})
		require.NoError(t, err)
		local, err := probes.FindByID(ctx, probe.LocalID)
		require.NoError(t, err)
		require.Equal(t, probe.StatusOffline, local.Status, "the local probe starts offline: nothing has connected")
		found, err := monitors.FindByID(ctx, workspacetest.ID, created.ID)
		require.NoError(t, err)
		require.Len(t, found.Locations, 1)
		online = found.Locations[0].Online
	}))
	return online
}

// AC4: the cluster on SQLite and PostgreSQL has no process of its own that runs checks, so its
// local probe is online by its instances, like a remote probe
func TestRegisterClusterAPI_TheLocalProbeIsOnlineByItsInstances(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	assert.False(t, localLocationOnline(t, container), "no instance is connected")
}

// Single mode is unchanged: its own process is the local probe, which is online whether or not
// anything connected
func TestRegisterSingle_TheLocalProbeIsTheProcessAndNeverOffline(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	assert.True(t, localLocationOnline(t, container), "the process runs the checks")
}

// In cluster mode the local probe is its instances, like a remote probe
func TestRegisterCluster_TheLocalProbeIsItsInstances(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(liveness shared.LocalProbeLiveness) {
		assert.Equal(t, shared.LocalProbeIsItsInstances, liveness)
	}))
}

// localProbeAvailability is the local probe's availability over 24 hours in the container's install
func localProbeAvailability(t *testing.T, container *dig.Container) *probe.Availability {
	t.Helper()
	var got *probe.Availability
	require.NoError(t, container.Invoke(func(availability probe.AvailabilityService, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))
		var err error
		got, err = availability.Availability(context.Background(), probe.LocalID, probe.Range24h)
		require.NoError(t, err)
	}))
	return got
}

// In single mode the process itself checks the local probe's monitors: it has no chart to draw,
// and the response says so
func TestRegisterSingle_TheLocalProbesAvailabilityIsInProcess(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	got := localProbeAvailability(t, container)

	assert.True(t, got.InProcess)
	assert.Empty(t, got.Periods)
	assert.Nil(t, got.UptimePercent)
	assert.Nil(t, got.TrackedFrom)
}

// In cluster mode the local probe's instances do, and it has periods like a remote probe
func TestRegisterCluster_TheLocalProbesAvailabilityComesFromItsHistory(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	got := localProbeAvailability(t, container)

	assert.False(t, got.InProcess)
	require.Len(t, got.Periods, 1, "the migration seeded it offline")
	assert.Equal(t, probe.StatusOffline, got.Periods[0].Status)
	require.NotNil(t, got.UptimePercent)
	assert.Equal(t, 0.0, *got.UptimePercent)
}

// AC1, AC2: in single mode the probes API lists the local probe online, and the stored status is
// left as it was, so that a move to cluster mode starts the local probe offline
func TestRegisterSingle_ListsTheLocalProbeOnline(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(controller *probe.Controller, probes probe.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))
		ctx := context.Background()

		listed, err := controller.GetProbes(ctx, &probe.GetProbesInput{})
		require.NoError(t, err)
		require.NotEmpty(t, listed.Body.Data)
		assert.Equal(t, probe.LocalID, listed.Body.Data[0].ID)
		assert.Equal(t, string(probe.StatusOnline), listed.Body.Data[0].Status, "the list says the local probe is online")

		request := httptest.NewRequest(http.MethodGet, "/api/v1/probes/local", nil)
		request.SetPathValue("id", probe.LocalID)
		got, err := controller.GetProbe(ctx, &probe.GetProbeInput{Request: request})
		require.NoError(t, err)
		assert.Equal(t, string(probe.StatusOnline), got.Body.Data.Status, "the probe says it is online")

		stored, err := probes.FindByID(ctx, probe.LocalID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOffline, stored.Status, "nothing writes online to the database")
	}))
}

package app

import (
	"context"
	"testing"

	"peekaping/internal/config"
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
		created, err := monitors.Create(ctx, &monitor.CreateUpdateDto{Type: "http", Name: "Home", Active: true, Config: "{}"})
		require.NoError(t, err)
		local, err := probes.FindByID(ctx, probe.LocalID)
		require.NoError(t, err)
		require.Equal(t, probe.StatusOffline, local.Status, "the local probe starts offline: nothing has connected")
		found, err := monitors.FindByID(ctx, created.ID)
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

// The MongoDB cluster keeps its producer, worker and ingester, which run the local probe's checks,
// so the local probe is the process there too. It has no probes at all (ADR 0001).
func TestRegisterCluster_OnMongoStorageTheLocalProbeIsTheProcess(t *testing.T) {
	for name, register := range map[string]func(*dig.Container) error{
		"api":      func(c *dig.Container) error { return RegisterClusterAPI(c, mongoClusterConfig(t)) },
		"producer": func(c *dig.Container) error { return RegisterClusterProducer(c, mongoClusterConfig(t)) },
	} {
		t.Run(name, func(t *testing.T) {
			container := dig.New()
			require.NoError(t, register(container))

			require.NoError(t, container.Invoke(func(liveness shared.LocalProbeLiveness) {
				assert.Equal(t, shared.LocalProbeIsTheProcess, liveness)
			}))
		})
	}
}

func TestRegisterCluster_OnSQLStorageTheLocalProbeIsItsInstances(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterAPI(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(liveness shared.LocalProbeLiveness) {
		assert.Equal(t, shared.LocalProbeIsItsInstances, liveness)
	}))
}

// mongoClusterConfig is a cluster binary's config on MongoDB. Nothing in the tests that use it
// opens the database.
func mongoClusterConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := clusterConfig(t)
	cfg.DBType = "mongo"
	return cfg
}

package app

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/events"
	"peekaping/internal/modules/probe"
	"peekaping/internal/modules/stale"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// The watchdog reads the probes' supported types from the probe service, in single mode
func TestRegisterSingle_TheWatchdogReadsTheTypesOfRealProbes(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterSingle(container, singleConfig(t)))

	require.NoError(t, container.Invoke(func(lookup stale.ProbeLookup, probes probe.Service, bus events.EventBus, db *bun.DB) {
		closeEventBus(t, bus)
		closeDB(t, db)
		require.NoError(t, migrateDB(db))

		assertProbeLookupReadsRealProbes(t, lookup, probes)
	}))
}

// and in cluster mode, where the producer's leader runs the watchdog
func TestRegisterClusterProducer_TheWatchdogReadsTheTypesOfRealProbes(t *testing.T) {
	container := dig.New()
	require.NoError(t, RegisterClusterProducer(container, clusterConfig(t)))

	require.NoError(t, container.Invoke(func(
		lookup stale.ProbeLookup,
		probes probe.Service,
		bus events.EventBus,
		db *bun.DB,
		client *asynq.Client,
		inspector *asynq.Inspector,
	) {
		closeEventBus(t, bus)
		closeDB(t, db)
		t.Cleanup(func() { assert.NoError(t, client.Close()) })
		t.Cleanup(func() { assert.NoError(t, inspector.Close()) })
		require.NoError(t, migrateDB(db))

		assertProbeLookupReadsRealProbes(t, lookup, probes)
	}))
}

// assertProbeLookupReadsRealProbes checks the watchdog's lookup against the real probe service: a
// probe's name and the types it reported, none for a probe that has not reported, and nil for a
// probe that doesn't exist.
func assertProbeLookupReadsRealProbes(t *testing.T, lookup stale.ProbeLookup, probes probe.Service) {
	t.Helper()
	ctx := context.Background()
	remote, err := probes.Create(ctx, &probe.CreateRequest{Name: "Frankfurt"})
	require.NoError(t, err)

	found, err := lookup.FindProbe(ctx, remote.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "Frankfurt", found.Name)
	assert.Empty(t, found.SupportedTypes, "a probe that has not reported its types runs every type")

	require.NoError(t, probes.RegisterInstance(ctx, remote.ID, &probe.Instance{ID: "instance-1", Hostname: "edge-1", LastSeenAt: time.Now().UTC()}, []string{"http", "tcp"}))
	found, err = lookup.FindProbe(ctx, remote.ID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, []string{"http", "tcp"}, found.SupportedTypes)

	missing, err := lookup.FindProbe(ctx, "no-such-probe")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// A MongoDB install has no probes: a lookup finds none, rather than failing the watchdog
func TestStaleProbeLookup_FindsNoProbeOnAnInstallWithoutProbes(t *testing.T) {
	lookup := provideStaleProbeLookup(unsupportedProbes{})

	found, err := lookup.FindProbe(context.Background(), "probe-1")

	require.NoError(t, err)
	assert.Nil(t, found)
}

// unsupportedProbes is the probe service of an install without probes
type unsupportedProbes struct{ probe.Service }

func (unsupportedProbes) FindByID(context.Context, string) (*probe.Model, error) {
	return nil, probe.ErrUnsupportedStorage
}

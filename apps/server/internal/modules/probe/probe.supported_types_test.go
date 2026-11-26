package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/probe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The types of two builds of one probe: the full image runs Docker monitors and the slim one does not
var (
	fullTypes = []string{"docker", "http", "tcp"}
	slimTypes = []string{"http", "tcp"}
)

// onEachBackend runs fn with a probe service over a migrated database of every SQL storage backend.
// PostgreSQL runs when PEE_TEST_POSTGRES_DSN is set.
func onEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		fn(t, fixture{svc: serviceOn(db, func(repo probe.Repository) probe.Repository { return repo }), db: db})
	})
}

func (f fixture) typesOf(t *testing.T, probeID string) []string {
	t.Helper()
	got, err := f.svc.FindByID(context.Background(), probeID)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got.SupportedTypes
}

// register has an instance say hello at the time, with the types of its build
func (f fixture) register(t *testing.T, probeID, instanceID string, seen time.Time, types []string) {
	t.Helper()
	require.NoError(t, f.svc.RegisterInstance(context.Background(), probeID, &probe.Instance{ID: instanceID, Hostname: instanceID, LastSeenAt: seen, SupportedTypes: types}))
}

// AC4: instances of one probe on different builds don't flip the probe's types, which are the
// union over its online instances
func TestService_TheProbesTypesAreTheUnionOverItsOnlineInstances(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created := f.create(t, "Frankfurt")
		now := time.Now().UTC()

		f.register(t, created.ID, "full", now, fullTypes)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID))

		f.register(t, created.ID, "slim", now, slimTypes)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "the slim build's hello doesn't take docker away")

		f.register(t, created.ID, "full", now.Add(time.Second), fullTypes)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "and the full build's hello again changes nothing")

		got, err := f.svc.FindByID(context.Background(), created.ID)
		require.NoError(t, err)
		byID := map[string][]string{}
		for _, instance := range got.Instances {
			byID[instance.ID] = instance.SupportedTypes
		}
		assert.Equal(t, map[string][]string{"full": fullTypes, "slim": slimTypes}, byID, "each instance keeps its own types")
	})
}

func TestService_AnInstanceThatSaysByeLeavesTheUnion(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created := f.create(t, "Frankfurt")
		now := time.Now().UTC()
		f.register(t, created.ID, "slim", now, slimTypes)
		f.register(t, created.ID, "full", now, fullTypes)
		require.Equal(t, fullTypes, f.typesOf(t, created.ID))

		require.NoError(t, f.svc.RetireInstance(context.Background(), created.ID, "full", probe.RetiredAt(now), now))

		assert.Equal(t, slimTypes, f.typesOf(t, created.ID), "docker leaves with the full build")

		f.register(t, created.ID, "full", now.Add(time.Second), fullTypes)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "and returns when it says hello again")
	})
}

func TestService_AnInstanceThatWentQuietLeavesTheUnion(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created := f.create(t, "Frankfurt")
		now := time.Now().UTC()
		f.register(t, created.ID, "full", now.Add(-2*probe.InstanceOnlineWindow), fullTypes)
		f.register(t, created.ID, "slim", now, slimTypes)

		assert.Equal(t, slimTypes, f.typesOf(t, created.ID), "an instance not heard from for a window is not online, though it never said bye")

		found, err := f.svc.TouchInstance(context.Background(), created.ID, "full", now)
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "any request from it brings it back, with no hello")
	})
}

// An empty list means every type, so an instance that reports none (an old probe) makes the union
// every type: the probe can be given any monitor, and one instance will check it
func TestService_AnInstanceThatListsNoTypesMakesTheUnionEveryType(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created := f.create(t, "Frankfurt")
		now := time.Now().UTC()
		f.register(t, created.ID, "old", now, nil)
		f.register(t, created.ID, "slim", now, slimTypes)

		assert.Equal(t, []string{}, f.typesOf(t, created.ID))
	})
}

// A probe with no instance online is as the probe was when it last had one, so that a probe that
// is down can't be given monitors it didn't run. A probe whose instances were pruned is too.
func TestService_AProbeWithNoInstanceOnlineKeepsTheTypesItLastRan(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created := f.create(t, "Frankfurt")
		assert.Equal(t, []string{}, f.typesOf(t, created.ID), "a probe that never connected runs every type")
		then := time.Now().UTC().Add(-time.Hour)
		f.register(t, created.ID, "full", then, fullTypes)
		f.register(t, created.ID, "slim", then, slimTypes)

		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "neither is online now, and the probe ran both builds when it was")

		pruned, err := probe.NewSQLRepository(f.db).DeleteInstancesUnseenSince(context.Background(), time.Now().UTC())
		require.NoError(t, err)
		require.Equal(t, 2, pruned)
		assert.Equal(t, fullTypes, f.typesOf(t, created.ID), "pruning the instances forgets nothing")
	})
}

func TestService_FindInstanceReturnsTheProbesInstanceWithItsTypes(t *testing.T) {
	onEachBackend(t, func(t *testing.T, f fixture) {
		created, other := f.create(t, "Frankfurt"), f.create(t, "Paris")
		f.register(t, created.ID, "slim", time.Now().UTC(), slimTypes)

		found, err := f.svc.FindInstance(context.Background(), created.ID, "slim")
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, slimTypes, found.SupportedTypes)

		missing, err := f.svc.FindInstance(context.Background(), other.ID, "slim")
		require.NoError(t, err)
		assert.Nil(t, missing, "another probe's instance is not found")
	})
}

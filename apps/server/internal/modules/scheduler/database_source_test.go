package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/check"
	"peekaping/internal/modules/events"
	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func idsOf(monitors []*shared.Monitor) []string {
	ids := make([]string, 0, len(monitors))
	for _, m := range monitors {
		ids = append(ids, m.ID)
	}
	return ids
}

func TestDatabaseSource_ListActiveListsOnlyTheActiveMonitorsTheLocalProbeRuns(t *testing.T) {
	t.Parallel()
	db := newFakeDB()
	db.put(monitorWith("a-local"))
	db.put(monitorWith("b-paused", paused))
	db.put(monitorWith("c-remote", onProbes("probe-1")))
	db.put(monitorWith("d-both", onProbes("probe-1", shared.LocalProbeID)))
	db.put(monitorWith("e-local"))
	source := NewDatabaseSource(db, db, newBus(t))

	first, err := source.ListActive(context.Background(), 0, 2)
	require.NoError(t, err)
	second, err := source.ListActive(context.Background(), 1, 2)
	require.NoError(t, err)
	third, err := source.ListActive(context.Background(), 2, 2)
	require.NoError(t, err)

	assert.Equal(t, []string{"a-local", "d-both"}, idsOf(first))
	assert.Equal(t, []string{"e-local"}, idsOf(second), "a short page ends the list")
	assert.Empty(t, third)
}

func TestDatabaseSource_FindReturnsTheMonitorOnlyWhileTheLocalProbeRunsIt(t *testing.T) {
	t.Parallel()
	db := newFakeDB()
	db.put(monitorWith("local"))
	db.put(monitorWith("both", onProbes("probe-1", shared.LocalProbeID)))
	db.put(monitorWith("paused", paused))
	db.put(monitorWith("remote", onProbes("probe-1")))
	source := NewDatabaseSource(db, db, newBus(t))

	for _, id := range []string{"local", "both", "paused"} {
		mon, err := source.Find(context.Background(), id)
		require.NoError(t, err)
		require.NotNil(t, mon, id)
		assert.Equal(t, id, mon.ID)
	}
	for name, id := range map[string]string{"on another probe": "remote", "gone": "gone"} {
		mon, err := source.Find(context.Background(), id)
		require.NoError(t, err, name)
		assert.Nil(t, mon, name)
	}
}

func TestDatabaseSource_FindReturnsTheDatabaseError(t *testing.T) {
	t.Parallel()
	failure := errors.New("database is down")
	source := NewDatabaseSource(failingMonitors{failure}, failingMonitors{failure}, newBus(t))

	mon, err := source.Find(context.Background(), "mon")

	assert.ErrorIs(t, err, failure)
	assert.Nil(t, mon)
}

func TestDatabaseSource_ListActiveReturnsTheDatabaseError(t *testing.T) {
	t.Parallel()
	failure := errors.New("database is down")
	source := NewDatabaseSource(failingMonitors{failure}, failingMonitors{failure}, newBus(t))

	_, err := source.ListActive(context.Background(), 0, 10)

	assert.ErrorIs(t, err, failure)
}

// failingMonitors is a monitor database and loader that fail every call
type failingMonitors struct{ err error }

func (f failingMonitors) FindByID(context.Context, string) (*shared.Monitor, error) {
	return nil, f.err
}

func (f failingMonitors) FindActiveByProbe(context.Context, string, int, int) ([]*shared.Monitor, error) {
	return nil, f.err
}

func (f failingMonitors) Load(context.Context, string) (*check.Job, error) { return nil, f.err }

func TestDatabaseSource_LoadIsTheLoadersCheck(t *testing.T) {
	t.Parallel()
	db := newFakeDB()
	db.put(monitorWith("mon", withInterval(30)))
	db.put(monitorWith("paused", paused))
	db.maintained = true
	source := NewDatabaseSource(db, db, newBus(t))

	job, err := source.Load(context.Background(), "mon")
	require.NoError(t, err)
	assert.Equal(t, "mon", job.Monitor.ID)
	assert.True(t, job.IsUnderMaintenance)

	_, err = source.Load(context.Background(), "gone")
	assert.ErrorIs(t, err, check.ErrMonitorNotFound)
	_, err = source.Load(context.Background(), "paused")
	assert.ErrorIs(t, err, check.ErrMonitorPaused)
}

func TestDatabaseSource_WatchRelaysTheMonitorEvents(t *testing.T) {
	t.Parallel()
	bus := newBus(t)
	source := NewDatabaseSource(newFakeDB(), newFakeDB(), bus)
	created, updated, deleted := make(chan string, 1), make(chan string, 1), make(chan string, 1)
	source.Watch(
		func(id string) { created <- id },
		func(id string) { updated <- id },
		func(id string) { deleted <- id },
	)
	ctx := context.Background()

	require.NoError(t, events.Publish(ctx, bus, events.MonitorCreated, shared.Monitor{ID: "new"}))
	require.NoError(t, events.Publish(ctx, bus, events.MonitorUpdated, shared.Monitor{ID: "edited"}))
	require.NoError(t, events.Publish(ctx, bus, events.MonitorDeleted, "gone"))

	assert.Equal(t, "new", receive(t, created))
	assert.Equal(t, "edited", receive(t, updated))
	assert.Equal(t, "gone", receive(t, deleted))
	for name, extra := range map[string]chan string{"created": created, "updated": updated, "deleted": deleted} {
		assert.Empty(t, extra, "%s is relayed once", name)
	}
}

func receive(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case id := <-ch:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("the event wasn't relayed")
		return ""
	}
}

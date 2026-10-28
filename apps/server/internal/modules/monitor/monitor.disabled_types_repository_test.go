package monitor

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-157: a monitor of a type the install turns off is kept and shown, but the queries that pick
// monitors to run, or to check for missed beats, skip it, and what is stored for it stays as it is.

func monitorOfType(name, monitorType string, probeIDs ...string) *shared.Monitor {
	monitor := monitorOn(name, true, probeIDs...)
	monitor.Type = monitorType
	return monitor
}

func TestSQLRepository_TheQueriesThatPickMonitorsToRunSkipADisabledType(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db, WithRepositoryDisabledTypes([]string{"docker", "snmp"}))
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		runs, err := repo.Create(ctx, testWorkspace, monitorOfType("Runs", "http", "local", "frankfurt"))
		require.NoError(t, err)
		_, err = repo.Create(ctx, testWorkspace, monitorOfType("Containers", "docker", "local", "frankfurt"))
		require.NoError(t, err)
		_, err = repo.Create(ctx, otherWorkspace, monitorOfType("Switches", "snmp", "local"))
		require.NoError(t, err)
		_, err = repo.Create(ctx, otherWorkspace, monitorOfType("Containers, no probe row", "docker"))
		require.NoError(t, err)

		active, err := repo.FindActiveAnyWorkspace(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{runs.ID}, idsOf(active))
		paged, err := repo.FindActivePaginatedAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{runs.ID}, idsOf(paged), "the watchdog's pages")
		for _, probe := range []string{shared.LocalProbeID, "frankfurt"} {
			onProbe, err := repo.FindActiveByProbeAnyWorkspace(ctx, probe, 0, 10)
			require.NoError(t, err)
			assert.Equal(t, []string{runs.ID}, idsOf(onProbe), "the monitors dealt to %s", probe)
		}

		// Everything else still sees them: a signed-in user's lists and pages, the usage ping
		mine, err := repo.FindAll(ctx, testWorkspace, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		assert.Len(t, mine, 2)
		all, err := repo.FindAllAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, all, 4)
	})
}

func TestSQLRepository_WithoutDisabledTypesTheQueriesPickEveryActiveMonitor(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		runs, err := repo.Create(ctx, testWorkspace, monitorOfType("Runs", "http", "local"))
		require.NoError(t, err)
		containers, err := repo.Create(ctx, testWorkspace, monitorOfType("Containers", "docker", "local"))
		require.NoError(t, err)

		active, err := repo.FindActiveAnyWorkspace(ctx)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{runs.ID, containers.ID}, idsOf(active))
		onProbe, err := repo.FindActiveByProbeAnyWorkspace(ctx, shared.LocalProbeID, 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{runs.ID, containers.ID}, idsOf(onProbe))
		assert.False(t, containers.TypeDisabled)
	})
}

// The reads of one monitor serve more than running it (a notification, a badge, the push URL, the
// list in the app), so they still find it, and say that its type is turned off. The monitor keeps
// what was stored for it, so it runs again once the type is turned back on.
func TestSQLRepository_AMonitorReadSaysWhetherItsTypeIsDisabled(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		before := NewSQLRepository(db)
		containers, err := before.Create(ctx, testWorkspace, monitorOfType("Containers", "docker", "local"))
		require.NoError(t, err)
		runs, err := before.Create(ctx, testWorkspace, monitorOfType("Runs", "http", "local"))
		require.NoError(t, err)
		repo := NewSQLRepository(db, WithRepositoryDisabledTypes([]string{"docker"}))

		byID, err := repo.FindByIDAnyWorkspace(ctx, containers.ID)
		require.NoError(t, err)
		require.NotNil(t, byID)
		assert.True(t, byID.TypeDisabled)
		assert.True(t, byID.Active, "it is still switched on")
		assert.Equal(t, []string{shared.LocalProbeID}, byID.ProbeIDs)
		inWorkspace, err := repo.FindByID(ctx, testWorkspace, containers.ID)
		require.NoError(t, err)
		assert.True(t, inWorkspace.TypeDisabled)
		listed, err := repo.FindAll(ctx, testWorkspace, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		disabled := map[string]bool{}
		for _, m := range listed {
			disabled[m.ID] = m.TypeDisabled
		}
		assert.Equal(t, map[string]bool{containers.ID: true, runs.ID: false}, disabled)

		// the flag is not stored: writing the monitor back, and reading it without the option, show nothing
		require.NoError(t, repo.UpdateFull(ctx, testWorkspace, containers.ID, byID))
		again, err := before.FindByID(ctx, testWorkspace, containers.ID)
		require.NoError(t, err)
		assert.False(t, again.TypeDisabled)
		assert.True(t, again.Active)
	})
}

// A result in flight when the type is turned off must not change the monitor: nothing is recorded on
// its rows, the judge is not asked, and the caller is told why
func TestSQLRepository_ALocationOfAMonitorOfADisabledTypeIsNotRecorded(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := NewSQLRepository(db, WithRepositoryDisabledTypes([]string{"docker"}))
		containers, err := repo.Create(ctx, testWorkspace, monitorOfType("Containers", "docker", "local"))
		require.NoError(t, err)

		judged := false
		err = repo.RecordLocationAnyWorkspace(ctx, containers.ID, stateOf("local", up, 12, time.Now()), func(shared.LocationJudgement) *shared.MonitorStatus {
			judged = true
			return statusPtr(up)
		})

		assert.ErrorIs(t, err, shared.ErrMonitorTypeDisabled)
		assert.False(t, judged)
		read, err := repo.FindByID(ctx, testWorkspace, containers.ID)
		require.NoError(t, err)
		require.Len(t, read.Locations, 1)
		assert.Nil(t, read.Locations[0].Status, "the row holds nothing")
	})
}

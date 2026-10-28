package monitor

import (
	"context"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// PP-155: the monitors of a suspended workspace are kept and shown, but the queries that pick
// monitors to run, or to check for missed beats, skip them.

func suspend(t *testing.T, db *bun.DB, id string, reason string) {
	t.Helper()
	_, err := db.Exec("UPDATE workspaces SET suspended_at = CURRENT_TIMESTAMP, suspended_reason = ? WHERE id = ?", reason, id)
	require.NoError(t, err)
}

func TestSQLRepository_TheQueriesThatPickMonitorsToRunSkipASuspendedWorkspace(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		runs, err := repo.Create(ctx, testWorkspace, monitorOn("Runs", true, "local", "frankfurt"))
		require.NoError(t, err)
		held, err := repo.Create(ctx, otherWorkspace, monitorOn("Held", true, "local", "frankfurt"))
		require.NoError(t, err)
		heldOnTheLocalProbeByDefault, err := repo.Create(ctx, otherWorkspace, createTestMonitor("Held, no probe row", true, shared.MonitorStatusPending))
		require.NoError(t, err)
		suspend(t, db, string(otherWorkspace), "unverified")

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

		// Everything else still sees them: a signed-in user's lists and pages, the usage ping, a public page
		mine, err := repo.FindAll(ctx, otherWorkspace, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{held.ID, heldOnTheLocalProbeByDefault.ID}, idsOf(mine))
		all, err := repo.FindAllAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.Len(t, all, 3)
		byID, err := repo.FindByID(ctx, otherWorkspace, held.ID)
		require.NoError(t, err)
		require.NotNil(t, byID)
		assert.True(t, byID.Active, "it is still switched on")
	})
}

func TestSQLRepository_ALiftedSuspensionPutsTheMonitorsBackInTheQueries(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		held, err := repo.Create(ctx, otherWorkspace, monitorOn("Held", true, "local"))
		require.NoError(t, err)
		suspend(t, db, string(otherWorkspace), "unverified")
		none, err := repo.FindActiveByProbeAnyWorkspace(ctx, shared.LocalProbeID, 0, 10)
		require.NoError(t, err)
		require.Empty(t, none)

		_, err = db.Exec("UPDATE workspaces SET suspended_at = NULL, suspended_reason = NULL WHERE id = ?", string(otherWorkspace))
		require.NoError(t, err)

		back, err := repo.FindActiveByProbeAnyWorkspace(ctx, shared.LocalProbeID, 0, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{held.ID}, idsOf(back))
	})
}

// The reads of one monitor by its id serve more than running it (a notification, a badge, the push
// URL), so they still find it, and say that its workspace is suspended
func TestSQLRepository_AMonitorReadByIDSaysWhetherItsWorkspaceIsSuspended(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		runs, err := repo.Create(ctx, testWorkspace, monitorWithToken("Runs", "runs-token"))
		require.NoError(t, err)
		held, err := repo.Create(ctx, otherWorkspace, monitorWithToken("Held", "held-token"))
		require.NoError(t, err)
		suspend(t, db, string(otherWorkspace), "unverified")

		byID, err := repo.FindByIDAnyWorkspace(ctx, held.ID)
		require.NoError(t, err)
		require.NotNil(t, byID)
		assert.True(t, byID.WorkspaceSuspended)
		inWorkspace, err := repo.FindByID(ctx, otherWorkspace, held.ID)
		require.NoError(t, err)
		assert.True(t, inWorkspace.WorkspaceSuspended)
		many, err := repo.FindByIDsAnyWorkspace(ctx, []string{runs.ID, held.ID})
		require.NoError(t, err)
		suspended := map[string]bool{}
		for _, m := range many {
			suspended[m.ID] = m.WorkspaceSuspended
		}
		assert.Equal(t, map[string]bool{runs.ID: false, held.ID: true}, suspended)
		byToken, err := repo.FindOneByPushTokenAnyWorkspace(ctx, "held-token")
		require.NoError(t, err)
		require.NotNil(t, byToken)
		assert.True(t, byToken.WorkspaceSuspended)
		running, err := repo.FindByIDAnyWorkspace(ctx, runs.ID)
		require.NoError(t, err)
		assert.False(t, running.WorkspaceSuspended)

		// writing the monitor back never stores the flag
		held.WorkspaceSuspended = false
		require.NoError(t, repo.UpdateFull(ctx, otherWorkspace, held.ID, held))
	})
}

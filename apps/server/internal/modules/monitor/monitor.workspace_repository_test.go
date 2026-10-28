package monitor

import (
	"context"
	"testing"

	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// A monitor is created in the workspace the call names, and a workspace in the monitor itself
// is ignored
func TestSQLRepository_CreateStoresTheMonitorInTheWorkspaceOfTheCall(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		claims := createTestMonitor("Claims", true, shared.MonitorStatusPending)
		claims.WorkspaceID = testWorkspace

		created, err := repo.Create(ctx, otherWorkspace, claims)

		require.NoError(t, err)
		assert.Equal(t, otherWorkspace, created.WorkspaceID)
		found, err := repo.FindByID(ctx, otherWorkspace, created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, otherWorkspace, found.WorkspaceID)
	})
}

func TestSQLRepository_ReadsOfAWorkspaceShowItsMonitorsOnly(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		mine, err := repo.Create(ctx, testWorkspace, createTestMonitor("Mine", true, shared.MonitorStatusPending))
		require.NoError(t, err)
		theirs, err := repo.Create(ctx, otherWorkspace, createTestMonitor("Theirs", true, shared.MonitorStatusPending))
		require.NoError(t, err)

		found, err := repo.FindByID(ctx, testWorkspace, theirs.ID)
		require.NoError(t, err)
		assert.Nil(t, found, "another workspace's monitor is not found")
		many, err := repo.FindByIDs(ctx, testWorkspace, []string{mine.ID, theirs.ID})
		require.NoError(t, err)
		assert.Equal(t, []string{mine.ID}, idsOf(many))
		all, err := repo.FindAll(ctx, testWorkspace, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{mine.ID}, idsOf(all))
		searched, err := repo.FindAll(ctx, testWorkspace, 0, 10, "theirs", nil, nil, nil)
		require.NoError(t, err)
		assert.Empty(t, searched)

		// The paths that run for the whole install see both
		anyID, err := repo.FindByIDAnyWorkspace(ctx, theirs.ID)
		require.NoError(t, err)
		require.NotNil(t, anyID)
		anyMany, err := repo.FindByIDsAnyWorkspace(ctx, []string{mine.ID, theirs.ID})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{mine.ID, theirs.ID}, idsOf(anyMany))
		anyAll, err := repo.FindAllAnyWorkspace(ctx, 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{mine.ID, theirs.ID}, idsOf(anyAll))
		active, err := repo.FindActiveAnyWorkspace(ctx)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{mine.ID, theirs.ID}, idsOf(active))
		onLocal, err := repo.FindActiveByProbeAnyWorkspace(ctx, shared.LocalProbeID, 0, 10)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{mine.ID, theirs.ID}, idsOf(onLocal))
		byToken, err := repo.FindOneByPushTokenAnyWorkspace(ctx, theirs.PushToken)
		require.NoError(t, err)
		require.NotNil(t, byToken)
		assert.Equal(t, theirs.ID, byToken.ID)
	})
}

func TestSQLRepository_AWriteToAnotherWorkspacesMonitorIsNotFoundAndChangesNothing(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		addProbe(t, db, "frankfurt")
		created, err := repo.Create(ctx, testWorkspace, monitorOn("Checkout", true, "local"))
		require.NoError(t, err)
		name, active := "Hijacked", false

		hijack := createTestMonitor("Hijacked", false, shared.MonitorStatusPending)
		hijack.ProbeIDs = []string{"frankfurt"}
		assert.ErrorIs(t, repo.UpdateFull(ctx, otherWorkspace, created.ID, hijack), ErrMonitorNotFound)
		assert.ErrorIs(t, repo.UpdatePartial(ctx, otherWorkspace, created.ID, &UpdateModel{Name: &name, Active: &active, ProbeIDs: []string{"frankfurt"}}), ErrMonitorNotFound)
		assert.ErrorIs(t, repo.UpdatePartial(ctx, otherWorkspace, created.ID, &UpdateModel{}), ErrMonitorNotFound, "even when there is nothing to write")
		assert.ErrorIs(t, repo.Delete(ctx, otherWorkspace, created.ID), ErrMonitorNotFound)
		assert.ErrorIs(t, repo.ResetLocations(ctx, otherWorkspace, created.ID), ErrMonitorNotFound)

		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "Checkout", found.Name)
		assert.True(t, found.Active)
		assert.Equal(t, []string{"local"}, found.ProbeIDs, "and its probes are as they were")
		assert.True(t, created.UpdatedAt.Equal(found.UpdatedAt))
	})
}

func TestSQLRepository_UpdatesAndDeletesInTheWorkspaceStillWork(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		created, err := repo.Create(ctx, testWorkspace, createTestMonitor("Before", true, shared.MonitorStatusPending))
		require.NoError(t, err)
		name := "After"

		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{Name: &name}))
		require.NoError(t, repo.UpdatePartial(ctx, testWorkspace, created.ID, &UpdateModel{}), "nothing to write is not an error for a monitor that is there")
		require.NoError(t, repo.UpdatePartialAnyWorkspace(ctx, created.ID, &UpdateModel{Name: &name}))
		found, err := repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "After", found.Name)
		assert.Equal(t, testWorkspace, found.WorkspaceID, "a write never moves the monitor")

		changed := createTestMonitor("Changed", true, shared.MonitorStatusPending)
		changed.WorkspaceID = otherWorkspace
		require.NoError(t, repo.UpdateFull(ctx, testWorkspace, created.ID, changed))
		found, err = repo.FindByID(ctx, testWorkspace, created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, testWorkspace, found.WorkspaceID, "a full update neither")

		require.NoError(t, repo.Delete(ctx, testWorkspace, created.ID))
		assert.ErrorIs(t, repo.Delete(ctx, testWorkspace, created.ID), ErrMonitorNotFound)
	})
}

func TestSQLRepository_AProxyReferenceIsClearedAndFoundInTheWorkspaceOnly(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := NewSQLRepository(db)
		ctx := context.Background()
		proxyID := uuid.NewString()
		_, err := db.Exec("INSERT INTO proxies (id, workspace_id, protocol, host, port) VALUES (?, ?, 'http', 'proxy.example.com', 8080)", proxyID, string(testWorkspace))
		require.NoError(t, err)
		viaProxy := func(name string) *Model {
			m := createTestMonitor(name, true, shared.MonitorStatusPending)
			m.ProxyId = proxyID
			return m
		}
		mine, err := repo.Create(ctx, testWorkspace, viaProxy("Mine"))
		require.NoError(t, err)
		theirs, err := repo.Create(ctx, otherWorkspace, viaProxy("Theirs"))
		require.NoError(t, err)

		users, err := repo.FindByProxyId(ctx, testWorkspace, proxyID)
		require.NoError(t, err)
		assert.Equal(t, []string{mine.ID}, idsOf(users))

		require.NoError(t, repo.RemoveProxyReference(ctx, testWorkspace, proxyID))

		cleared, err := repo.FindByID(ctx, testWorkspace, mine.ID)
		require.NoError(t, err)
		assert.Empty(t, cleared.ProxyId)
		kept, err := repo.FindByID(ctx, otherWorkspace, theirs.ID)
		require.NoError(t, err)
		assert.Equal(t, proxyID, kept.ProxyId, "another workspace's monitor keeps its proxy")
	})
}

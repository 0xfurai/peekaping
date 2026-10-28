package workspace_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

const monitorWorkspaceMigration = "20261103120000"

func storedMonitor(t *testing.T, db *bun.DB, name string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', ?, 60, 10, 1, 10, 0)`, id, name)
	require.NoError(t, err)
	return id
}

func TestMigrations_TheMonitorsOfAnUpgradedInstallBelongToItsWorkspace(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		first, second := storedMonitor(t, db, "First"), storedMonitor(t, db, "Second")

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		only := workspaces(t, db)
		require.Len(t, only, 1)
		owners := map[string]string{}
		var rows []struct {
			ID          string `bun:"id"`
			WorkspaceID string `bun:"workspace_id"`
		}
		require.NoError(t, db.NewSelect().Table("monitors").Column("id", "workspace_id").Scan(ctx, &rows))
		for _, row := range rows {
			owners[row.ID] = row.WorkspaceID
		}
		assert.Equal(t, map[string]string{first: only[0].ID, second: only[0].ID}, owners)
	})
}

func TestMigrations_AMonitorWithoutAWorkspaceIsRefused(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, monitorWorkspaceMigration)
		insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'No workspace', 60, 10, 1, 10, 0)`, uuid.NewString())
		assert.Error(t, err)
	})
}

func TestMigrations_TheMonitorWorkspaceGoesDownAndComesBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, monitorWorkspaceMigration)
		_, err := upgrade.Migrate(ctx) // only this migration is left to run
		require.NoError(t, err)
		workspaceID := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Kept')", workspaceID)
		require.NoError(t, err)
		id := uuid.NewString()
		_, err = db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Kept', 60, 10, 1, 10, 0)`, id, workspaceID)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO monitor_probes (monitor_id, probe_id) VALUES (?, 'local')", id)
		require.NoError(t, err)

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT workspace_id FROM monitors")
		assert.Error(t, err, "the column goes")
		var name string
		require.NoError(t, db.NewSelect().Table("monitors").Column("name").Where("id = ?", id).Scan(ctx, &name), "and the monitor stays")
		assert.Equal(t, "Kept", name)
		var links int
		require.NoError(t, db.NewSelect().Table("monitor_probes").ColumnExpr("COUNT(*)").Where("monitor_id = ?", id).Scan(ctx, &links), "with its probes")
		assert.Equal(t, 1, links)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		var kept string
		require.NoError(t, db.NewSelect().Table("monitors").Column("workspace_id").Where("id = ?", id).Scan(ctx, &kept), "and comes back")
		assert.Equal(t, workspaceID, kept, "with the monitor in the install's one workspace")
	})
}

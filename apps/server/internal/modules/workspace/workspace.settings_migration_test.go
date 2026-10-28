package workspace_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The data retention period and the certificate expiry notification days move from the install's
// settings to its workspace's (PP-150, docs/adr/0005 section 5).

const settingsMigration = "20261106120000"

type settingRow struct {
	Key   string `bun:"key"`
	Value string `bun:"value"`
	Type  string `bun:"type"`
}

func installSettings(t *testing.T, db *bun.DB) map[string]settingRow {
	t.Helper()
	var rows []settingRow
	require.NoError(t, db.NewSelect().Table("settings").Column("key", "value", "type").Scan(context.Background(), &rows))
	got := map[string]settingRow{}
	for _, row := range rows {
		got[row.Key] = row
	}
	return got
}

func workspaceSettings(t *testing.T, db *bun.DB) map[string]settingRow {
	t.Helper()
	var rows []settingRow
	require.NoError(t, db.NewSelect().Table("workspace_settings").Column("key", "value", "type").Scan(context.Background(), &rows))
	got := map[string]settingRow{}
	for _, row := range rows {
		got[row.Key] = row
	}
	return got
}

func storeInstallSetting(t *testing.T, db *bun.DB, key, value, valueType string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO settings (key, value, type) VALUES (?, ?, ?)", key, value, valueType)
	require.NoError(t, err)
}

// addWorkspaceAt adds a workspace the way the upgrade does for an install that has data
func addWorkspaceAt(t *testing.T, db *bun.DB, name string, createdAt time.Time) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec("INSERT INTO workspaces (id, name, created_at) VALUES (?, ?, ?)", id, name, createdAt)
	require.NoError(t, err)
	return id
}

func TestMigrations_TheRetentionPeriodAndCertificateDaysOfAnUpgradedInstallMoveToItsWorkspace(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, settingsMigration)
		addWorkspaceAt(t, db, "Newer", time.Now())
		oldest := addWorkspaceAt(t, db, "Oldest", time.Now().Add(-time.Hour))
		storeInstallSetting(t, db, "KEEP_DATA_PERIOD_DAYS", "30", "int")
		storeInstallSetting(t, db, "cert_expiry_notify_days", "[3,30]", "json")
		storeInstallSetting(t, db, "ACCESS_TOKEN_EXPIRED_IN", "15m", "string")

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Equal(t, map[string]settingRow{
			"KEEP_DATA_PERIOD_DAYS":   {Key: "KEEP_DATA_PERIOD_DAYS", Value: "30", Type: "int"},
			"cert_expiry_notify_days": {Key: "cert_expiry_notify_days", Value: "[3,30]", Type: "json"},
		}, workspaceSettings(t, db), "in the oldest workspace")
		var owners []string
		require.NoError(t, db.NewSelect().Table("workspace_settings").Column("workspace_id").Where("key = 'KEEP_DATA_PERIOD_DAYS'").Scan(ctx, &owners))
		assert.Equal(t, []string{oldest}, owners)
		assert.Equal(t, map[string]settingRow{
			"ACCESS_TOKEN_EXPIRED_IN": {Key: "ACCESS_TOKEN_EXPIRED_IN", Value: "15m", Type: "string"},
		}, installSettings(t, db), "the install keeps its other settings")
	})
}

func TestMigrations_AnUpgradedInstallThatNeverStoredThemKeepsAYearAndTheDefaultDays(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, settingsMigration)
		addWorkspaceAt(t, db, "Only", time.Now())

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Equal(t, map[string]settingRow{
			"KEEP_DATA_PERIOD_DAYS":   {Key: "KEEP_DATA_PERIOD_DAYS", Value: "365", Type: "int"},
			"cert_expiry_notify_days": {Key: "cert_expiry_notify_days", Value: "[7,14,21]", Type: "json"},
		}, workspaceSettings(t, db))
	})
}

func TestMigrations_AFreshInstallLosesTheKeysAndGetsNoWorkspaceSettings(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, settingsMigration)
		storeInstallSetting(t, db, "KEEP_DATA_PERIOD_DAYS", "180", "int")
		storeInstallSetting(t, db, "cert_expiry_notify_days", "[7,14,21]", "json")
		require.Empty(t, workspaces(t, db))

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Empty(t, installSettings(t, db))
		assert.Empty(t, workspaceSettings(t, db))
		assert.Empty(t, workspaces(t, db))
	})
}

func TestMigrations_MovingTheSettingsAgainChangesNothing(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, settingsMigration)
		addWorkspaceAt(t, db, "Only", time.Now())
		storeInstallSetting(t, db, "KEEP_DATA_PERIOD_DAYS", "30", "int")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("UPDATE workspace_settings SET value = '90' WHERE key = 'KEEP_DATA_PERIOD_DAYS'")
		require.NoError(t, err)

		// A re-run: the migration is no longer recorded, and the install has the old value again
		_, err = db.Exec("DELETE FROM bun_migrations WHERE name = ?", settingsMigration)
		require.NoError(t, err)
		storeInstallSetting(t, db, "KEEP_DATA_PERIOD_DAYS", "7", "int")
		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Equal(t, "90", workspaceSettings(t, db)["KEEP_DATA_PERIOD_DAYS"].Value, "what the workspace holds stays")
		assert.Len(t, workspaceSettings(t, db), 2)
		assert.Empty(t, installSettings(t, db))
	})
}

func TestMigrations_TheWorkspaceSettingsGoDownAndComeBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, settingsMigration)
		addWorkspaceAt(t, db, "Only", time.Now())
		storeInstallSetting(t, db, "KEEP_DATA_PERIOD_DAYS", "30", "int")
		storeInstallSetting(t, db, "cert_expiry_notify_days", "[3]", "json")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("UPDATE workspace_settings SET value = '45' WHERE key = 'KEEP_DATA_PERIOD_DAYS'")
		require.NoError(t, err)

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]settingRow{
			"KEEP_DATA_PERIOD_DAYS":   {Key: "KEEP_DATA_PERIOD_DAYS", Value: "45", Type: "int"},
			"cert_expiry_notify_days": {Key: "cert_expiry_notify_days", Value: "[3]", Type: "json"},
		}, installSettings(t, db), "the oldest workspace's values go back to the install")
		assert.Empty(t, workspaceSettings(t, db))

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		assert.Equal(t, "45", workspaceSettings(t, db)["KEEP_DATA_PERIOD_DAYS"].Value, "and come back")
		assert.Equal(t, "[3]", workspaceSettings(t, db)["cert_expiry_notify_days"].Value)
		assert.Empty(t, installSettings(t, db))
	})
}

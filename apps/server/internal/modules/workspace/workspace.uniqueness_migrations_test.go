package workspace_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The migrations that tie the tables of a monitor to the monitor and make push tokens and custom
// domains unique across the install (PP-146, docs/adr/0005 section 6)
const (
	monitorChildrenMigration = "20261101120200"
	pushTokensMigration      = "20261101120300"
	customDomainsMigration   = "20261101120400"
)

func insertMonitor(t *testing.T, db *bun.DB, id string) {
	t.Helper()
	_, err := db.Exec(records["monitors"], id)
	require.NoError(t, err)
}

func insertTLSInfo(t *testing.T, db *bun.DB, monitorID, info string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO monitor_tls_info (id, monitor_id, info_json) VALUES (?, ?, ?)", uuid.NewString(), monitorID, info)
	require.NoError(t, err)
}

func insertSentNotification(t *testing.T, db *bun.DB, monitorID string, days int) {
	t.Helper()
	_, err := db.Exec("INSERT INTO notification_sent_history (id, type, monitor_id, days) VALUES (?, 'certificate', ?, ?)", uuid.NewString(), monitorID, days)
	require.NoError(t, err)
}

// childRows are the monitor ids of the rows in a table, with one column of what the row holds
func childRows(t *testing.T, db *bun.DB, table, column string) map[string]string {
	t.Helper()
	var rows []struct {
		MonitorID string `bun:"monitor_id"`
		Value     string `bun:"value"`
	}
	require.NoError(t, db.NewSelect().Table(table).Column("monitor_id").ColumnExpr("CAST("+column+" AS TEXT) AS value").Scan(context.Background(), &rows))
	got := map[string]string{}
	for _, r := range rows {
		got[r.MonitorID] = r.Value
	}
	return got
}

func TestMigrations_TLSInfoAndNotificationHistoryOfADeletedMonitorAreRemoved(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, monitorChildrenMigration)
		kept, gone := uuid.NewString(), uuid.NewString()
		insertMonitor(t, db, kept)
		insertTLSInfo(t, db, kept, `{"kept":true}`)
		insertTLSInfo(t, db, gone, `{"kept":false}`)
		insertTLSInfo(t, db, "not-a-uuid", `{"kept":false}`)
		insertSentNotification(t, db, kept, 7)
		_, err := db.Exec("UPDATE notification_sent_history SET created_at = '2026-01-02 03:04:05+00:00' WHERE monitor_id = ?", kept)
		require.NoError(t, err)
		insertSentNotification(t, db, gone, 7)
		insertSentNotification(t, db, "not-a-uuid", 7)

		var before string
		require.NoError(t, db.NewSelect().Table("notification_sent_history").ColumnExpr("CAST(created_at AS TEXT)").Where("monitor_id = ?", kept).Scan(ctx, &before))

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		var after string
		require.NoError(t, db.NewSelect().Table("notification_sent_history").ColumnExpr("CAST(created_at AS TEXT)").Where("monitor_id = ?", kept).Scan(ctx, &after))
		assert.Equal(t, before, after, "a row keeps its time")
		assert.Equal(t, map[string]string{kept: `{"kept":true}`}, childRows(t, db, "monitor_tls_info", "info_json"))
		assert.Equal(t, map[string]string{kept: "7"}, childRows(t, db, "notification_sent_history", "days"))
	})
}

func TestMigrations_DeletingAMonitorDeletesItsTLSInfoAndNotificationHistory(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, monitorChildrenMigration)
		keep, drop := uuid.NewString(), uuid.NewString()
		for _, id := range []string{keep, drop} {
			insertMonitor(t, db, id)
			insertTLSInfo(t, db, id, `{}`)
			insertSentNotification(t, db, id, 14)
		}
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		_, err = db.Exec("DELETE FROM monitors WHERE id = ?", drop)
		require.NoError(t, err)

		assert.Equal(t, map[string]string{keep: "{}"}, childRows(t, db, "monitor_tls_info", "info_json"))
		assert.Equal(t, map[string]string{keep: "14"}, childRows(t, db, "notification_sent_history", "days"))
	})
}

func TestMigrations_ARowForAMonitorThatDoesNotExistIsRefused(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		upgrade := migratedBefore(t, db, monitorChildrenMigration)
		_, err := upgrade.Migrate(context.Background())
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO monitor_tls_info (id, monitor_id, info_json) VALUES (?, ?, '{}')", uuid.NewString(), uuid.NewString())
		assert.Error(t, err)
		_, err = db.Exec("INSERT INTO notification_sent_history (id, type, monitor_id, days) VALUES (?, 'certificate', ?, 7)", uuid.NewString(), uuid.NewString())
		assert.Error(t, err)
	})
}

func TestMigrations_TLSInfoAndNotificationHistoryKeepTheirUniqueRulesAndGoBackDown(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, monitorChildrenMigration)
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		id := uuid.NewString()
		insertMonitor(t, db, id)
		insertTLSInfo(t, db, id, `{}`)
		insertSentNotification(t, db, id, 7)

		_, err = db.Exec("INSERT INTO monitor_tls_info (id, monitor_id, info_json) VALUES (?, ?, '{}')", uuid.NewString(), id)
		assert.Error(t, err, "one TLS info row for a monitor")
		_, err = db.Exec("INSERT INTO notification_sent_history (id, type, monitor_id, days) VALUES (?, 'certificate', ?, 7)", uuid.NewString(), id)
		assert.Error(t, err, "one notification for a type, monitor and days")

		// Down: the rows stay, and a row for a monitor that does not exist is accepted again
		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{id: "{}"}, childRows(t, db, "monitor_tls_info", "info_json"))
		assert.Equal(t, map[string]string{id: "7"}, childRows(t, db, "notification_sent_history", "days"))
		insertTLSInfo(t, db, uuid.NewString(), `{}`)
	})
}

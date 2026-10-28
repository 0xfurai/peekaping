package workspace_test

import (
	"context"
	"fmt"
	"testing"

	"peekaping/cmd/bun/migrations"

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

// runMigrationAgain runs a Go migration a second time, as a re-run of the upgrade would
func runMigrationAgain(db *bun.DB, name string) error {
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name == name {
			return m.Up(context.Background(), db, nil)
		}
	}
	return fmt.Errorf("migration %s not found", name)
}

type pushMonitor struct {
	id, name, typ string
	token         *string // push_token as stored
	config        *string
}

func ptr(s string) *string { return &s }

func insertPushMonitor(t *testing.T, db *bun.DB, m pushMonitor) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, push_token, config) VALUES (?, ?, ?, 60, 10, 1, 10, 0, ?, ?)`,
		m.id, m.typ, m.name, m.token, m.config)
	require.NoError(t, err)
}

// pushTokens are the stored push tokens by monitor id, with nil for NULL
func pushTokens(t *testing.T, db *bun.DB) map[string]*string {
	t.Helper()
	var rows []struct {
		ID    string  `bun:"id"`
		Token *string `bun:"push_token"`
	}
	require.NoError(t, db.NewSelect().Table("monitors").Column("id", "push_token").Scan(context.Background(), &rows))
	got := map[string]*string{}
	for _, r := range rows {
		got[r.ID] = r.Token
	}
	return got
}

func migrationApplied(t *testing.T, db *bun.DB, name string) bool {
	t.Helper()
	exists, err := db.NewSelect().Table("bun_migrations").Where("name = ?", name).Exists(context.Background())
	require.NoError(t, err)
	return exists
}

func TestMigrations_PushTokensAreFilledFromTheConfigAndEmptyOnesBecomeNull(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, pushTokensMigration)
		fromConfig, emptyEverywhere, notPush, own, noConfig, brokenConfig, nullToken :=
			uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertPushMonitor(t, db, pushMonitor{fromConfig, "From config", "push", ptr(""), ptr(`{"pushToken":"config-token"}`)})
		insertPushMonitor(t, db, pushMonitor{emptyEverywhere, "Empty", "push", ptr(""), ptr(`{"pushToken":""}`)})
		insertPushMonitor(t, db, pushMonitor{notPush, "Not push", "http", ptr(""), ptr(`{"pushToken":"ignored-token"}`)})
		insertPushMonitor(t, db, pushMonitor{own, "Own", "push", ptr("own-token"), ptr(`{"pushToken":"other-token"}`)})
		insertPushMonitor(t, db, pushMonitor{noConfig, "No config", "push", ptr(""), nil})
		insertPushMonitor(t, db, pushMonitor{brokenConfig, "Odd config", "push", ptr(""), ptr(`{"pushToken":5}`)})
		insertPushMonitor(t, db, pushMonitor{nullToken, "Null", "push", nil, ptr(`{"pushToken":"null-token"}`)})

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Equal(t, map[string]*string{
			fromConfig:      ptr("config-token"),
			emptyEverywhere: nil,
			notPush:         nil,
			own:             ptr("own-token"),
			noConfig:        nil,
			brokenConfig:    nil,
			nullToken:       ptr("null-token"),
		}, pushTokens(t, db))

		// The database holds each token once from now on, and any number of monitors without one
		_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, push_token) VALUES (?, 'push', 'Twin', 60, 10, 1, 10, 0, 'own-token')`, uuid.NewString())
		assert.Error(t, err)
		_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, push_token) VALUES (?, 'push', 'Another without', 60, 10, 1, 10, 0, NULL)`, uuid.NewString())
		assert.NoError(t, err)
	})
}

func TestMigrations_TwoMonitorsWithOnePushTokenStopTheUpgradeBeforeItChangesAnything(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, pushTokensMigration)
		first, second, third, other, empty := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
		insertPushMonitor(t, db, pushMonitor{first, "First pusher", "push", ptr("shared-secret"), nil})
		// The second holds it only in its config, which the upgrade would copy into push_token
		insertPushMonitor(t, db, pushMonitor{second, "Second pusher", "push", ptr(""), ptr(`{"pushToken":"shared-secret"}`)})
		insertPushMonitor(t, db, pushMonitor{third, "Third pusher", "push", ptr("shared-secret"), nil})
		insertPushMonitor(t, db, pushMonitor{other, "Other", "push", ptr("unique-token"), nil})
		insertPushMonitor(t, db, pushMonitor{empty, "Empty", "push", ptr(""), ptr(`{"pushToken":"fill-me"}`)})
		before := pushTokens(t, db)

		_, err := upgrade.Migrate(ctx)

		require.Error(t, err)
		for _, want := range []string{"First pusher", first, "Second pusher", second, "Third pusher", third} {
			assert.Contains(t, err.Error(), want)
		}
		assert.NotContains(t, err.Error(), "shared-secret", "the token is a secret")
		assert.NotContains(t, err.Error(), "unique-token")
		assert.NotContains(t, err.Error(), "Other")
		assert.Contains(t, err.Error(), "new push token", "it says what to do")
		assert.Equal(t, before, pushTokens(t, db), "nothing changed, not even the token that could be filled")
		assert.False(t, migrationApplied(t, db, pushTokensMigration))
		_, err = db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, push_token) VALUES (?, 'push', 'Twin', 60, 10, 1, 10, 0, 'unique-token')`, uuid.NewString())
		assert.NoError(t, err, "no unique index yet")
	})
}

func TestMigrations_RunningThePushTokenMigrationAgainChangesNothing(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, pushTokensMigration)
		insertPushMonitor(t, db, pushMonitor{uuid.NewString(), "From config", "push", ptr(""), ptr(`{"pushToken":"config-token"}`)})
		insertPushMonitor(t, db, pushMonitor{uuid.NewString(), "Empty", "push", ptr(""), nil})
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		want := pushTokens(t, db)

		require.NoError(t, runMigrationAgain(db, pushTokensMigration))

		assert.Equal(t, want, pushTokens(t, db))
	})
}

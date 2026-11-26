package modules_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"peekaping/internal/modules/workspace/workspacetest"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
	"peekaping/internal/modules/heartbeat"
	"peekaping/internal/modules/maintenance"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/monitor_maintenance"
	"peekaping/internal/modules/monitor_notification"
	"peekaping/internal/modules/monitor_tag"
	"peekaping/internal/modules/notification_channel"
	"peekaping/internal/modules/proxy"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/modules/stats"
	"peekaping/internal/modules/tag"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// errRollBack is what a transaction test returns from WithTx to roll its writes back.
var errRollBack = errors.New("roll back")

// forEachTxBackend runs fn on a migrated SQLite database and, when PEE_TEST_POSTGRES_DSN is
// set, on a migrated PostgreSQL schema. Both have one connection, as SQLite does in the app,
// so a repository call inside a transaction that doesn't run on that transaction waits for the
// connection the transaction holds, and fails when ctx times out instead of passing unnoticed.
func forEachTxBackend(t *testing.T, fn func(t *testing.T, ctx context.Context, db *bun.DB)) {
	run := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			db.SetMaxOpenConns(1)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t.Cleanup(cancel)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			workspacetest.OldestOrAdd(t, db)
			fn(t, ctx, db)
		}
	}
	t.Run("SQLite", run(func(t *testing.T) *bun.DB {
		db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "transaction.db")}, zap.NewNop().Sugar())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		return db
	}))
	t.Run("Postgres", run(func(t *testing.T) *bun.DB {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pee86_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
		db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
		t.Cleanup(func() {
			require.NoError(t, db.Close())
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		return db
	}))
}

func insertTxMonitor(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.OldestOrAdd(t, db)
	_, err := db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
	require.NoError(t, err)
	return id
}

func insertTxTag(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.InsertTag(t, db, id, "tag-"+id)
	return id
}

func insertTxChannel(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.InsertChannel(t, db, id)
	return id
}

func insertTxProxy(t *testing.T, db *bun.DB) string {
	t.Helper()
	id := uuid.NewString()
	workspacetest.InsertProxy(t, db, id)
	return id
}

func TestMonitorTagRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := monitor_tag.NewSQLRepository(db)
		monitorID, tagA, tagB := insertTxMonitor(t, db), insertTxTag(t, db), insertTxTag(t, db)
		kept, err := repo.Create(ctx, &monitor_tag.Model{MonitorID: monitorID, TagID: tagA})
		require.NoError(t, err)

		// Every method runs in the transaction: each one sees what the one before it wrote there.
		err = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			added, err := repo.Create(ctx, &monitor_tag.Model{MonitorID: monitorID, TagID: tagB})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the link Create wrote")
			byMonitor, err := repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Len(t, byMonitor, 2, "FindByMonitorID sees both links")
			byTag, err := repo.FindByTagID(ctx, tagB)
			require.NoError(t, err)
			assert.Len(t, byTag, 1, "FindByTagID sees the new link")

			require.NoError(t, repo.Delete(ctx, added.ID))
			require.NoError(t, repo.DeleteByMonitorAndTag(ctx, monitorID, tagA))
			byMonitor, err = repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Empty(t, byMonitor, "Delete and DeleteByMonitorAndTag removed both links")

			_, err = repo.Create(ctx, &monitor_tag.Model{MonitorID: monitorID, TagID: tagA})
			require.NoError(t, err)
			require.NoError(t, repo.DeleteByTagID(ctx, tagA))
			byTag, err = repo.FindByTagID(ctx, tagA)
			require.NoError(t, err)
			assert.Empty(t, byTag, "DeleteByTagID removed the link")

			_, err = repo.Create(ctx, &monitor_tag.Model{MonitorID: monitorID, TagID: tagB})
			require.NoError(t, err)
			require.NoError(t, repo.DeleteByMonitorID(ctx, monitorID))
			byMonitor, err = repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Empty(t, byMonitor, "DeleteByMonitorID removed the link")

			_, err = repo.Create(ctx, &monitor_tag.Model{MonitorID: monitorID, TagID: tagB})
			require.NoError(t, err)
			return errRollBack
		})

		require.ErrorIs(t, err, errRollBack)
		links, err := repo.FindByMonitorID(ctx, monitorID)
		require.NoError(t, err)
		require.Len(t, links, 1, "the rollback undid every write: the deletes and the last link")
		assert.Equal(t, kept.ID, links[0].ID)
	})
}

func TestMonitorNotificationRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := monitor_notification.NewSQLRepository(db)
		monitorID, channelA, channelB := insertTxMonitor(t, db), insertTxChannel(t, db), insertTxChannel(t, db)
		kept, err := repo.Create(ctx, &monitor_notification.Model{MonitorID: monitorID, NotificationID: channelA})
		require.NoError(t, err)

		err = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			added, err := repo.Create(ctx, &monitor_notification.Model{MonitorID: monitorID, NotificationID: channelB})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the link Create wrote")
			byMonitor, err := repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Len(t, byMonitor, 2, "FindByMonitorID sees both links")

			require.NoError(t, repo.Delete(ctx, added.ID))
			require.NoError(t, repo.DeleteByNotificationID(ctx, channelA))
			byMonitor, err = repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Empty(t, byMonitor, "Delete and DeleteByNotificationID removed both links")

			_, err = repo.Create(ctx, &monitor_notification.Model{MonitorID: monitorID, NotificationID: channelB})
			require.NoError(t, err)
			require.NoError(t, repo.DeleteByMonitorID(ctx, monitorID))
			byMonitor, err = repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Empty(t, byMonitor, "DeleteByMonitorID removed the link")

			_, err = repo.Create(ctx, &monitor_notification.Model{MonitorID: monitorID, NotificationID: channelB})
			require.NoError(t, err)
			return errRollBack
		})

		require.ErrorIs(t, err, errRollBack)
		links, err := repo.FindByMonitorID(ctx, monitorID)
		require.NoError(t, err)
		require.Len(t, links, 1, "the rollback undid every write: the deletes and the last link")
		assert.Equal(t, kept.ID, links[0].ID)
	})
}

func TestMonitorRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := monitor.NewSQLRepository(db)
		proxyID, tagID := insertTxProxy(t, db), insertTxTag(t, db)
		newMonitor := func(name, pushToken string) *monitor.Model {
			return &monitor.Model{Type: "push", Name: name, Interval: 60, Timeout: 16, MaxRetries: 1, RetryInterval: 60, Active: true, Config: `{}`, ProxyId: proxyID, PushToken: pushToken}
		}
		kept, err := repo.Create(ctx, workspacetest.ID, newMonitor("kept", "kept-token"))
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO monitor_tags (id, monitor_id, tag_id) VALUES (?, ?, ?)", uuid.NewString(), kept.ID, tagID)
		require.NoError(t, err)

		// Every method runs in the transaction: each one sees what the ones before it wrote there.
		err = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			added, err := repo.Create(ctx, workspacetest.ID, newMonitor("added", "added-token"))
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the monitor Create wrote")
			byIDs, err := repo.FindByIDs(ctx, workspacetest.ID, []string{kept.ID, added.ID})
			require.NoError(t, err)
			assert.Len(t, byIDs, 2, "FindByIDs sees both monitors")
			all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "", nil, nil, nil)
			require.NoError(t, err)
			assert.Len(t, all, 2, "FindAll sees both monitors")
			tagged, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "", nil, nil, []string{tagID})
			require.NoError(t, err)
			assert.Len(t, tagged, 1, "FindAll by tag joins the committed link")
			active, err := repo.FindActiveAnyWorkspace(ctx)
			require.NoError(t, err)
			assert.Len(t, active, 2, "FindActive sees both monitors")
			page, err := repo.FindActivePaginatedAnyWorkspace(ctx, 0, 10)
			require.NoError(t, err)
			assert.Len(t, page, 2, "FindActivePaginated sees both monitors")
			byToken, err := repo.FindOneByPushTokenAnyWorkspace(ctx, "added-token")
			require.NoError(t, err)
			require.NotNil(t, byToken, "FindOneByPushToken sees the monitor Create wrote")
			assert.Equal(t, added.ID, byToken.ID)
			byProxy, err := repo.FindByProxyId(ctx, workspacetest.ID, proxyID)
			require.NoError(t, err)
			assert.Len(t, byProxy, 2, "FindByProxyId sees both monitors")

			renamed := *added
			renamed.Name = "added-renamed"
			require.NoError(t, repo.UpdateFull(ctx, workspacetest.ID, added.ID, &renamed))
			found, err = repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			assert.Equal(t, "added-renamed", found.Name, "UpdateFull updated the monitor Create wrote")
			name := "kept-renamed"
			require.NoError(t, repo.UpdatePartial(ctx, workspacetest.ID, kept.ID, &monitor.UpdateModel{Name: &name}))
			found, err = repo.FindByID(ctx, workspacetest.ID, kept.ID)
			require.NoError(t, err)
			assert.Equal(t, "kept-renamed", found.Name, "UpdatePartial updated the committed monitor")

			require.NoError(t, repo.RemoveProxyReference(ctx, workspacetest.ID, proxyID))
			byProxy, err = repo.FindByProxyId(ctx, workspacetest.ID, proxyID)
			require.NoError(t, err)
			assert.Empty(t, byProxy, "RemoveProxyReference cleared both monitors' proxy")

			require.NoError(t, repo.Delete(ctx, workspacetest.ID, added.ID))
			found, err = repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			assert.Nil(t, found, "Delete removed the monitor Create wrote")
			return errRollBack
		})

		require.ErrorIs(t, err, errRollBack)
		all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "", nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, all, 1, "the rollback undid the Create")
		assert.Equal(t, kept.ID, all[0].ID)
		assert.Equal(t, "kept", all[0].Name, "the rollback undid UpdatePartial")
		assert.Equal(t, proxyID, all[0].ProxyId, "the rollback undid RemoveProxyReference")
	})
}

func TestHeartbeatRepository_DeleteByMonitorIDRunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := heartbeat.NewSQLRepository(db)
		monitorID := insertTxMonitor(t, db)
		for _, msg := range []string{"first", "second"} {
			_, err := repo.Create(ctx, &heartbeat.Model{MonitorID: monitorID, Status: shared.MonitorStatusUp, Msg: msg})
			require.NoError(t, err)
		}
		stored := func(ctx context.Context) int {
			t.Helper()
			n, err := dbtx.DB(ctx, db).NewSelect().Table("heartbeats").Where("monitor_id = ?", monitorID).Count(ctx)
			require.NoError(t, err)
			return n
		}
		transactor := dbtx.NewSQLTransactor(db)

		err := transactor.WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, repo.DeleteByMonitorID(ctx, monitorID))
			assert.Zero(t, stored(ctx), "the transaction sees the heartbeats deleted")
			return errRollBack
		})
		require.ErrorIs(t, err, errRollBack)
		assert.Equal(t, 2, stored(ctx), "the rollback kept the heartbeats")

		err = transactor.WithTx(ctx, func(ctx context.Context) error {
			return repo.DeleteByMonitorID(ctx, monitorID)
		})
		require.NoError(t, err)
		assert.Zero(t, stored(ctx), "the commit deleted the heartbeats")
	})
}

func TestStatsRepository_DeleteByMonitorIDRunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := stats.NewSQLRepository(db)
		monitorID := insertTxMonitor(t, db)
		periods := []stats.StatPeriod{stats.StatMinutely, stats.StatHourly, stats.StatDaily}
		at := time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)
		for _, period := range periods {
			require.NoError(t, repo.AddToStat(ctx, &stats.Stat{MonitorID: monitorID, Timestamp: at, Up: 1, PingCount: 1, Ping: 100, PingMin: 100, PingMax: 100}, period))
		}
		stored := func(ctx context.Context, period stats.StatPeriod) int {
			t.Helper()
			table := map[stats.StatPeriod]string{stats.StatMinutely: "stat_minutely", stats.StatHourly: "stat_hourly", stats.StatDaily: "stat_daily"}[period]
			n, err := dbtx.DB(ctx, db).NewSelect().Table(table).Where("monitor_id = ?", monitorID).Count(ctx)
			require.NoError(t, err)
			return n
		}
		transactor := dbtx.NewSQLTransactor(db)

		err := transactor.WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, repo.DeleteByMonitorID(ctx, monitorID))
			for _, period := range periods {
				assert.Zero(t, stored(ctx, period), "the transaction sees the %s stats deleted", period)
			}
			return errRollBack
		})
		require.ErrorIs(t, err, errRollBack)
		for _, period := range periods {
			assert.Equal(t, 1, stored(ctx, period), "the rollback kept the %s stats", period)
		}

		err = transactor.WithTx(ctx, func(ctx context.Context) error {
			return repo.DeleteByMonitorID(ctx, monitorID)
		})
		require.NoError(t, err)
		for _, period := range periods {
			assert.Zero(t, stored(ctx, period), "the commit deleted the %s stats", period)
		}
	})
}

// inTxWithin runs fn in a transaction that rolls back, under a ctx that times out soon: a
// repository that ignores the transaction waits for the one connection it holds, so the test
// fails at the deadline instead of hanging.
func inTxWithin(ctx context.Context, db *bun.DB, fn func(ctx context.Context)) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
		fn(ctx)
		return errRollBack
	})
}

func TestTagRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := tag.NewSQLRepository(db)
		kept, err := repo.Create(ctx, workspacetest.ID, &tag.Model{Name: "kept", Color: "#111111"})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			added, err := repo.Create(ctx, workspacetest.ID, &tag.Model{Name: "added", Color: "#222222"})
			require.NoError(t, err)
			byName, err := repo.FindByName(ctx, workspacetest.ID, "added")
			require.NoError(t, err)
			require.NotNil(t, byName, "FindByName sees the tag Create wrote")
			all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
			require.NoError(t, err)
			assert.Len(t, all, 2, "FindAll sees both tags")
			renamed := *added
			renamed.Name = "added-renamed"
			require.NoError(t, repo.UpdateFull(ctx, workspacetest.ID, added.ID, &renamed))
			name := "kept-renamed"
			require.NoError(t, repo.UpdatePartial(ctx, workspacetest.ID, kept.ID, &tag.UpdateModel{Name: &name}))
			found, err := repo.FindByID(ctx, workspacetest.ID, kept.ID)
			require.NoError(t, err)
			assert.Equal(t, "kept-renamed", found.Name, "UpdatePartial updated the committed tag")
			require.NoError(t, repo.Delete(ctx, workspacetest.ID, added.ID))
		})

		require.ErrorIs(t, err, errRollBack)
		all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
		require.NoError(t, err)
		require.Len(t, all, 1, "the rollback undid the Create")
		assert.Equal(t, "kept", all[0].Name, "the rollback undid UpdatePartial")
	})
}

func TestNotificationChannelRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := notification_channel.NewSQLRepository(db)
		config := `{}`
		kept, err := repo.Create(ctx, workspacetest.ID, &notification_channel.Model{Name: "kept", Type: "webhook", Active: true, Config: &config})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			added, err := repo.Create(ctx, workspacetest.ID, &notification_channel.Model{Name: "added", Type: "webhook", Active: true, Config: &config})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the channel Create wrote")
			all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
			require.NoError(t, err)
			assert.Len(t, all, 2, "FindAll sees both channels")
			renamed := *added
			renamed.Name = "added-renamed"
			require.NoError(t, repo.UpdateFull(ctx, workspacetest.ID, added.ID, &renamed))
			name := "kept-renamed"
			require.NoError(t, repo.UpdatePartial(ctx, workspacetest.ID, kept.ID, &notification_channel.UpdateModel{Name: &name}))
			require.NoError(t, repo.Delete(ctx, workspacetest.ID, added.ID))
		})

		require.ErrorIs(t, err, errRollBack)
		all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
		require.NoError(t, err)
		require.Len(t, all, 1, "the rollback undid the Create")
		assert.Equal(t, "kept", all[0].Name, "the rollback undid UpdatePartial")
	})
}

func TestMaintenanceRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := maintenance.NewSQLRepository(db)
		kept, err := repo.Create(ctx, workspacetest.ID, &maintenance.Model{Title: "kept", Strategy: "manual", Active: true})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			added, err := repo.Create(ctx, workspacetest.ID, &maintenance.Model{Title: "added", Strategy: "manual", Active: true})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the window Create wrote")
			all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "", "")
			require.NoError(t, err)
			assert.Len(t, all, 2, "FindAll sees both windows")
			renamed := *added
			renamed.Title = "added-renamed"
			_, err = repo.UpdateFull(ctx, workspacetest.ID, added.ID, &renamed)
			require.NoError(t, err)
			title := "kept-renamed"
			_, err = repo.UpdatePartial(ctx, workspacetest.ID, kept.ID, &maintenance.UpdateModel{Title: &title})
			require.NoError(t, err)
			_, err = repo.SetActive(ctx, workspacetest.ID, kept.ID, false)
			require.NoError(t, err)
			require.NoError(t, repo.Delete(ctx, workspacetest.ID, added.ID))
		})

		require.ErrorIs(t, err, errRollBack)
		found, err := repo.FindByID(ctx, workspacetest.ID, kept.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "kept", found.Title, "the rollback undid UpdatePartial")
		assert.True(t, found.Active, "the rollback undid SetActive")
		all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "", "")
		require.NoError(t, err)
		assert.Len(t, all, 1, "the rollback undid the Create")
	})
}

func TestMonitorMaintenanceRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := monitor_maintenance.NewSQLRepository(db)
		monitorID := insertTxMonitor(t, db)
		windowA, windowB := uuid.NewString(), uuid.NewString()
		workspacetest.InsertMaintenance(t, db, windowA)
		workspacetest.InsertMaintenance(t, db, windowB)
		kept, err := repo.Create(ctx, &monitor_maintenance.Model{MonitorID: monitorID, MaintenanceID: windowA})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			added, err := repo.Create(ctx, &monitor_maintenance.Model{MonitorID: monitorID, MaintenanceID: windowB})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the link Create wrote")
			byMonitor, err := repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Len(t, byMonitor, 2, "FindByMonitorID sees both links")
			byWindow, err := repo.FindByMaintenanceID(ctx, windowB)
			require.NoError(t, err)
			assert.Len(t, byWindow, 1, "FindByMaintenanceID sees the new link")
			inWorkspace, err := repo.FindMonitorIDsInWorkspace(ctx, workspacetest.ID, []string{monitorID})
			require.NoError(t, err)
			assert.Equal(t, []string{monitorID}, inWorkspace)

			require.NoError(t, repo.Delete(ctx, added.ID))
			require.NoError(t, repo.DeleteByMaintenanceID(ctx, windowA))
			byMonitor, err = repo.FindByMonitorID(ctx, monitorID)
			require.NoError(t, err)
			assert.Empty(t, byMonitor, "Delete and DeleteByMaintenanceID removed both links")
			_, err = repo.Create(ctx, &monitor_maintenance.Model{MonitorID: monitorID, MaintenanceID: windowB})
			require.NoError(t, err)
			require.NoError(t, repo.DeleteByMonitorID(ctx, monitorID))
		})

		require.ErrorIs(t, err, errRollBack)
		links, err := repo.FindByMonitorID(ctx, monitorID)
		require.NoError(t, err)
		require.Len(t, links, 1, "the rollback undid every write")
		assert.Equal(t, kept.ID, links[0].ID)
	})
}

func TestProxyRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := proxy.NewSQLRepository(db)
		kept, err := repo.Create(ctx, workspacetest.ID, &proxy.Model{Protocol: "http", Host: "kept.example", Port: 3128})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			added, err := repo.Create(ctx, workspacetest.ID, &proxy.Model{Protocol: "http", Host: "added.example", Port: 3128})
			require.NoError(t, err)
			found, err := repo.FindByID(ctx, workspacetest.ID, added.ID)
			require.NoError(t, err)
			require.NotNil(t, found, "FindByID sees the proxy Create wrote")
			all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
			require.NoError(t, err)
			assert.Len(t, all, 2, "FindAll sees both proxies")
			renamed := *added
			renamed.Host = "added-renamed.example"
			_, err = repo.UpdateFull(ctx, workspacetest.ID, added.ID, &renamed)
			require.NoError(t, err)
			host := "kept-renamed.example"
			updated, err := repo.UpdatePartial(ctx, workspacetest.ID, kept.ID, &proxy.UpdateModel{Host: &host})
			require.NoError(t, err)
			assert.Equal(t, host, updated.Host, "UpdatePartial updated the committed proxy")
			require.NoError(t, repo.Delete(ctx, workspacetest.ID, added.ID))
		})

		require.ErrorIs(t, err, errRollBack)
		all, err := repo.FindAll(ctx, workspacetest.ID, 0, 10, "")
		require.NoError(t, err)
		require.Len(t, all, 1, "the rollback undid the Create")
		assert.Equal(t, "kept.example", all[0].Host, "the rollback undid UpdatePartial")
	})
}

func TestWorkspaceSettingRepository_RunsInTheTransactionInCtx(t *testing.T) {
	forEachTxBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		repo := setting.NewSQLWorkspaceRepository(db)
		_, err := repo.SetByKey(ctx, workspacetest.ID, "kept", &setting.Model{Value: "1", Type: "int"})
		require.NoError(t, err)

		err = inTxWithin(ctx, db, func(ctx context.Context) {
			_, err := repo.SetByKey(ctx, workspacetest.ID, "added", &setting.Model{Value: "2", Type: "int"})
			require.NoError(t, err)
			_, err = repo.SetByKey(ctx, workspacetest.ID, "kept", &setting.Model{Value: "3", Type: "int"})
			require.NoError(t, err)
			found, err := repo.GetByKey(ctx, workspacetest.ID, "added")
			require.NoError(t, err)
			require.NotNil(t, found, "GetByKey sees the setting SetByKey wrote")
			require.NoError(t, repo.DeleteByKey(ctx, workspacetest.ID, "added"))
			found, err = repo.GetByKey(ctx, workspacetest.ID, "added")
			require.NoError(t, err)
			assert.Nil(t, found, "DeleteByKey removed it")
		})

		require.ErrorIs(t, err, errRollBack)
		kept, err := repo.GetByKey(ctx, workspacetest.ID, "kept")
		require.NoError(t, err)
		require.NotNil(t, kept)
		assert.Equal(t, "1", kept.Value, "the rollback undid the second SetByKey")
		added, err := repo.GetByKey(ctx, workspacetest.ID, "added")
		require.NoError(t, err)
		assert.Nil(t, added, "the rollback undid the first")
	})
}

package monitor_maintenance_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor_maintenance"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "monitor_maintenance.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "monitor_maintenance_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
}

// fixture is the service linking monitors to maintenance windows, plus constructors for the rows a link points at.
type fixture struct {
	svc     monitor_maintenance.Service
	monitor func() string
	window  func() string
}

// forEachBackend runs fn against that service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			fn(t, fixture{
				svc: monitor_maintenance.NewService(monitor_maintenance.NewSQLRepository(db), zap.NewNop().Sugar()),
				monitor: func() string {
					id := uuid.NewString()
					_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
					require.NoError(t, err)
					return id
				},
				window: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO maintenances (id, title, strategy) VALUES (?, 'Upgrade', 'manual')", id)
					require.NoError(t, err)
					return id
				},
			})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

func monitorsOf(t *testing.T, svc monitor_maintenance.Service, windowID string) []string {
	t.Helper()
	ids, err := svc.GetMonitors(context.Background(), windowID)
	require.NoError(t, err)
	return ids
}

func windowsOf(t *testing.T, svc monitor_maintenance.Service, monitorID string) []string {
	t.Helper()
	links, err := svc.FindByMonitorID(context.Background(), monitorID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.MaintenanceID)
	}
	return ids
}

func TestMaintenanceWindowMonitors_SetMonitorsReplacesTheWholeSet(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		upgrade, backup := f.window(), f.window()
		web, db, api := f.monitor(), f.monitor(), f.monitor()
		require.NoError(t, f.svc.SetMonitors(ctx, backup, []string{db}))

		require.NoError(t, f.svc.SetMonitors(ctx, upgrade, []string{web, db}))
		assert.ElementsMatch(t, []string{web, db}, monitorsOf(t, f.svc, upgrade))

		require.NoError(t, f.svc.SetMonitors(ctx, upgrade, []string{db, api}))
		assert.ElementsMatch(t, []string{db, api}, monitorsOf(t, f.svc, upgrade), "web is dropped and api added")
		assert.ElementsMatch(t, []string{db}, monitorsOf(t, f.svc, backup), "another maintenance window keeps its monitors")

		require.NoError(t, f.svc.SetMonitors(ctx, upgrade, []string{}))
		assert.Empty(t, monitorsOf(t, f.svc, upgrade))
		assert.ElementsMatch(t, []string{db}, monitorsOf(t, f.svc, backup))
	})
}

func TestMaintenanceWindowMonitors_LinksAreListedAndDeletedFromBothSides(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		upgrade, backup, audit := f.window(), f.window(), f.window()
		web, db, api := f.monitor(), f.monitor(), f.monitor()
		webUpgrade, err := f.svc.Create(ctx, web, upgrade)
		require.NoError(t, err)
		for _, pair := range [][2]string{{web, backup}, {web, audit}, {db, upgrade}, {api, upgrade}, {db, backup}} {
			_, err = f.svc.Create(ctx, pair[0], pair[1])
			require.NoError(t, err)
		}

		found, err := f.svc.FindByID(ctx, webUpgrade.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, web, found.MonitorID)
		assert.Equal(t, upgrade, found.MaintenanceID)
		assert.ElementsMatch(t, []string{upgrade, backup, audit}, windowsOf(t, f.svc, web))

		require.NoError(t, f.svc.Delete(ctx, webUpgrade.ID))
		assert.ElementsMatch(t, []string{backup, audit}, windowsOf(t, f.svc, web), "Delete removes that one link")

		require.NoError(t, f.svc.DeleteByMonitorID(ctx, web))
		assert.Empty(t, windowsOf(t, f.svc, web))
		assert.ElementsMatch(t, []string{db}, monitorsOf(t, f.svc, backup), "DeleteByMonitorID keeps other monitors' links")

		require.NoError(t, f.svc.DeleteByMaintenanceID(ctx, upgrade))
		assert.Empty(t, monitorsOf(t, f.svc, upgrade))
		assert.ElementsMatch(t, []string{backup}, windowsOf(t, f.svc, db), "DeleteByMaintenanceID keeps other maintenance windows' links")
	})
}

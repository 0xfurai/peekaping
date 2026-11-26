package monitor_status_page_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"peekaping/internal/modules/workspace/workspacetest"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor_status_page"
	"peekaping/internal/modules/workspace"

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
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "monitor_status_page.db")}, zap.NewNop().Sugar())
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
	schema := "monitor_status_page_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// fixture is a monitor-status-page service plus constructors for the rows a link points at.
type fixture struct {
	svc     monitor_status_page.Service
	monitor func() string
	page    func() string
	// workspace is the workspace monitor() puts its monitors in, and other one with a monitor of its own
	workspace, other workspace.ID
	otherMonitor     func() string
}

// forEachBackend runs fn against a monitor-status-page service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, f fixture)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			oldest := workspace.ID(workspacetest.OldestOrAdd(t, db))
			workspacetest.Add(t, db, workspacetest.Other, "Other")
			fn(t, fixture{
				workspace: oldest, other: workspacetest.Other,
				otherMonitor: func() string {
					id := uuid.NewString()
					_, err := db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, ?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id, string(workspacetest.Other))
					require.NoError(t, err)
					return id
				},
				svc: monitor_status_page.NewService(monitor_status_page.NewSQLRepository(db), zap.NewNop().Sugar()),
				monitor: func() string {
					id := uuid.NewString()
					workspacetest.OldestOrAdd(t, db)
					_, err := db.Exec(`INSERT INTO monitors (id, workspace_id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, (SELECT id FROM workspaces ORDER BY created_at, id LIMIT 1), 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
					require.NoError(t, err)
					return id
				},
				page: func() string {
					id := uuid.NewString()
					workspacetest.InsertStatusPage(t, db, id, "page-"+id)
					return id
				},
			})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

func add(t *testing.T, svc monitor_status_page.Service, pageID, monitorID string, order int) {
	t.Helper()
	_, err := svc.AddMonitorToStatusPage(context.Background(), pageID, monitorID, order, true)
	require.NoError(t, err)
}

// Order and visibility are left out: SQL doesn't store them yet (PP-71).
func monitorsOn(t *testing.T, svc monitor_status_page.Service, pageID string) []string {
	t.Helper()
	links, err := svc.GetMonitorsForStatusPage(context.Background(), pageID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.MonitorID)
	}
	return ids
}

func pagesOf(t *testing.T, svc monitor_status_page.Service, monitorID string) []string {
	t.Helper()
	links, err := svc.GetStatusPagesForMonitor(context.Background(), monitorID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.StatusPageID)
	}
	return ids
}

func TestMonitorStatusPages_LinksAreListedFromBothSides(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		public, internal := f.page(), f.page()
		web, db := f.monitor(), f.monitor()
		add(t, f.svc, public, web, 0)
		add(t, f.svc, public, db, 1)
		add(t, f.svc, internal, db, 0)

		assert.ElementsMatch(t, []string{web, db}, monitorsOn(t, f.svc, public))
		assert.ElementsMatch(t, []string{db}, monitorsOn(t, f.svc, internal))
		assert.ElementsMatch(t, []string{public}, pagesOf(t, f.svc, web))
		assert.ElementsMatch(t, []string{public, internal}, pagesOf(t, f.svc, db))

		found, err := f.svc.FindByStatusPageAndMonitor(context.Background(), internal, db)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, internal, found.StatusPageID)
		assert.Equal(t, db, found.MonitorID)

		missing, err := f.svc.FindByStatusPageAndMonitor(context.Background(), internal, web)
		require.NoError(t, err)
		assert.Nil(t, missing)
	})
}

func TestMonitorStatusPages_RemovesOnlyTheLinksTheyName(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		public, internal := f.page(), f.page()
		web, db := f.monitor(), f.monitor()
		add(t, f.svc, public, web, 0)
		add(t, f.svc, public, db, 1)
		add(t, f.svc, internal, web, 0)
		add(t, f.svc, internal, db, 1)

		require.NoError(t, f.svc.RemoveMonitorFromStatusPage(ctx, public, web))
		assert.ElementsMatch(t, []string{db}, monitorsOn(t, f.svc, public))
		assert.ElementsMatch(t, []string{web, db}, monitorsOn(t, f.svc, internal), "the same monitor stays on other pages")

		require.NoError(t, f.svc.DeleteAllMonitorsForStatusPage(ctx, internal))
		assert.Empty(t, monitorsOn(t, f.svc, internal))
		assert.ElementsMatch(t, []string{db}, monitorsOn(t, f.svc, public), "other pages keep their monitors")
	})
}

// A page shows only the monitors of its workspace: one of another workspace, one that doesn't exist
// and one whose ID is not even a UUID are all unknown, on every database
func TestMonitorStatusPages_RequireMonitorsKnowsOnlyTheWorkspacesOwn(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		mine, alsoMine, theirs := f.monitor(), f.monitor(), f.otherMonitor()

		assert.NoError(t, f.svc.RequireMonitors(ctx, f.workspace, nil))
		assert.NoError(t, f.svc.RequireMonitors(ctx, f.workspace, []string{mine, alsoMine, mine}))
		assert.NoError(t, f.svc.RequireMonitors(ctx, f.other, []string{theirs}))
		for name, id := range map[string]string{
			"another workspace's": theirs,
			"unknown":             uuid.NewString(),
			"not a UUID":          "not-a-uuid",
			"empty":               "",
		} {
			err := f.svc.RequireMonitors(ctx, f.workspace, []string{mine, id})
			assert.ErrorIs(t, err, monitor_status_page.ErrUnknownMonitor, name)
			if err != nil && id != "" {
				assert.Contains(t, err.Error(), id, name)
			}
		}
		assert.ErrorIs(t, f.svc.RequireMonitors(ctx, f.other, []string{mine}), monitor_status_page.ErrUnknownMonitor, "and the other way round")
	})
}

package monitor_tag_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"
	"peekaping/internal/modules/monitor_tag"

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
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "monitor_tag.db")}, zap.NewNop().Sugar())
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
	schema := "monitor_tag_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// fixture is a monitor-tag service plus constructors for the rows a link points at.
type fixture struct {
	svc     monitor_tag.Service
	monitor func() string
	tag     func() string
}

// forEachBackend runs fn against a monitor-tag service on every database backend.
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
				svc: monitor_tag.NewService(monitor_tag.NewSQLRepository(db), zap.NewNop().Sugar()),
				monitor: func() string {
					id := uuid.NewString()
					_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`, id)
					require.NoError(t, err)
					return id
				},
				tag: func() string {
					id := uuid.NewString()
					_, err := db.Exec("INSERT INTO tags (id, name, color) VALUES (?, ?, '#3B82F6')", id, "tag-"+id)
					require.NoError(t, err)
					return id
				},
			})
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

func link(t *testing.T, svc monitor_tag.Service, monitorID, tagID string) *monitor_tag.Model {
	t.Helper()
	created, err := svc.Create(context.Background(), monitorID, tagID)
	require.NoError(t, err)
	return created
}

func tagsOf(t *testing.T, svc monitor_tag.Service, monitorID string) []string {
	t.Helper()
	links, err := svc.FindByMonitorID(context.Background(), monitorID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.TagID)
	}
	return ids
}

func monitorsOf(t *testing.T, svc monitor_tag.Service, tagID string) []string {
	t.Helper()
	links, err := svc.FindByTagID(context.Background(), tagID)
	require.NoError(t, err)
	ids := []string{}
	for _, l := range links {
		ids = append(ids, l.MonitorID)
	}
	return ids
}

func TestMonitorTags_LinksAreListedFromBothSides(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		web, db := f.monitor(), f.monitor()
		prod, eu := f.tag(), f.tag()
		created := link(t, f.svc, web, prod)
		link(t, f.svc, web, eu)
		link(t, f.svc, db, prod)

		found, err := f.svc.FindByID(context.Background(), created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, web, found.MonitorID)
		assert.Equal(t, prod, found.TagID)

		assert.ElementsMatch(t, []string{prod, eu}, tagsOf(t, f.svc, web))
		assert.ElementsMatch(t, []string{prod}, tagsOf(t, f.svc, db))
		assert.ElementsMatch(t, []string{web, db}, monitorsOf(t, f.svc, prod))
		assert.ElementsMatch(t, []string{web}, monitorsOf(t, f.svc, eu))
	})
}

// tagsByMonitor lists the tag IDs of each monitor's links, in the order they came.
func tagsByMonitor(links map[string][]*monitor_tag.Model) map[string][]string {
	byMonitor := map[string][]string{}
	for monitorID, monitorLinks := range links {
		for _, l := range monitorLinks {
			byMonitor[monitorID] = append(byMonitor[monitorID], l.TagID)
		}
	}
	return byMonitor
}

func TestMonitorTags_LinksOfManyMonitorsAreListedAtOnce(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		web, db, idle, other := f.monitor(), f.monitor(), f.monitor(), f.monitor()
		prod, eu := f.tag(), f.tag()
		link(t, f.svc, web, prod)
		link(t, f.svc, web, eu)
		link(t, f.svc, db, eu)
		link(t, f.svc, other, prod)

		found, err := f.svc.FindByMonitorIDs(ctx, []string{web, db, idle})
		require.NoError(t, err)

		byMonitor := tagsByMonitor(found)
		assert.Len(t, byMonitor, 2, "the monitor with no links and the monitor not asked for have no entry")
		assert.ElementsMatch(t, []string{prod, eu}, byMonitor[web])
		assert.Equal(t, []string{eu}, byMonitor[db])
		assert.Equal(t, tagsOf(t, f.svc, web), byMonitor[web], "each monitor's links come in the order FindByMonitorID lists them")

		for _, l := range found[web] {
			assert.Equal(t, web, l.MonitorID)
		}

		none, err := f.svc.FindByMonitorIDs(ctx, nil)
		require.NoError(t, err)
		assert.Empty(t, none, "no monitors asked for, no links")
	})
}

func TestMonitorTags_SQLListsLinksNewestFirst(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		web, db := f.monitor(), f.monitor()
		prod, eu, edge := f.tag(), f.tag(), f.tag()
		// The repository stamps a link with the time it is created, so each link needs its own
		link(t, f.svc, web, prod)
		time.Sleep(10 * time.Millisecond)
		link(t, f.svc, db, prod)
		time.Sleep(10 * time.Millisecond)
		link(t, f.svc, web, eu)
		time.Sleep(10 * time.Millisecond)
		link(t, f.svc, db, eu)
		time.Sleep(10 * time.Millisecond)
		link(t, f.svc, web, edge)

		found, err := f.svc.FindByMonitorIDs(context.Background(), []string{web, db})
		require.NoError(t, err)

		byMonitor := tagsByMonitor(found)
		assert.Equal(t, []string{edge, eu, prod}, byMonitor[web], "FindByMonitorIDs: the newest link first")
		assert.Equal(t, []string{eu, prod}, byMonitor[db])
		assert.Equal(t, []string{edge, eu, prod}, tagsOf(t, f.svc, web), "FindByMonitorID: the newest link first")
	})
}

func TestMonitorTags_DeletesRemoveOnlyTheLinksTheyName(t *testing.T) {
	forEachBackend(t, func(t *testing.T, f fixture) {
		ctx := context.Background()
		web, db, api := f.monitor(), f.monitor(), f.monitor()
		prod, eu, staging := f.tag(), f.tag(), f.tag()
		webProd := link(t, f.svc, web, prod)
		for _, pair := range [][2]string{{web, eu}, {web, staging}, {db, prod}, {db, eu}, {db, staging}, {api, prod}, {api, eu}} {
			link(t, f.svc, pair[0], pair[1])
		}

		require.NoError(t, f.svc.Delete(ctx, webProd.ID))
		assert.ElementsMatch(t, []string{eu, staging}, tagsOf(t, f.svc, web), "Delete removes that one link")
		gone, err := f.svc.FindByID(ctx, webProd.ID)
		require.NoError(t, err)
		assert.Nil(t, gone)

		require.NoError(t, f.svc.DeleteByMonitorAndTag(ctx, db, eu))
		assert.ElementsMatch(t, []string{prod, staging}, tagsOf(t, f.svc, db), "DeleteByMonitorAndTag removes that pair")

		require.NoError(t, f.svc.DeleteByMonitorID(ctx, api))
		assert.Empty(t, tagsOf(t, f.svc, api))
		assert.ElementsMatch(t, []string{db}, monitorsOf(t, f.svc, prod), "DeleteByMonitorID keeps other monitors' links")

		require.NoError(t, f.svc.DeleteByTagID(ctx, staging))
		assert.Empty(t, monitorsOf(t, f.svc, staging))
		assert.ElementsMatch(t, []string{eu}, tagsOf(t, f.svc, web), "DeleteByTagID keeps other tags' links")
		assert.ElementsMatch(t, []string{prod}, tagsOf(t, f.svc, db))
	})
}

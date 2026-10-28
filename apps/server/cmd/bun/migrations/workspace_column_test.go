package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/infra"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// forEachBackend runs fn on an empty SQLite database and on an empty PostgreSQL schema (skipped
// without PEE_TEST_POSTGRES_DSN), both migrated up to the migration before the one that ties API
// keys to a workspace, so that the workspaces table is there.
func forEachBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	t.Run("SQLite", func(t *testing.T) {
		db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "column.db")}, zap.NewNop().Sugar())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		fn(t, migratedWithoutAPIKeyWorkspace(t, db))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "column_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		fn(t, migratedWithoutAPIKeyWorkspace(t, db))
	})
}

func migratedWithoutAPIKeyWorkspace(t *testing.T, db *bun.DB) *bun.DB {
	t.Helper()
	ctx := context.Background()
	before := migrate.NewMigrations()
	for _, m := range Migrations.Sorted() {
		if m.Name < "20261102120000" {
			before.Add(m)
		}
	}
	migrator := migrate.NewMigrator(db, before)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
	return db
}

// widgets is a table of its own, so that the helper is tested on more than api_keys
func createWidgets(t *testing.T, db *bun.DB, names ...string) {
	t.Helper()
	_, err := db.Exec("CREATE TABLE widgets (id UUID PRIMARY KEY, name VARCHAR(50) NOT NULL UNIQUE, note TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("CREATE INDEX idx_widgets_note ON widgets(note)")
	require.NoError(t, err)
	for _, name := range names {
		_, err = db.Exec("INSERT INTO widgets (id, name, note) VALUES (?, ?, 'n')", uuid.NewString(), name)
		require.NoError(t, err)
	}
}

func insertWorkspace(t *testing.T, db *bun.DB, name, createdAt string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec("INSERT INTO workspaces (id, name, created_at) VALUES (?, ?, ?)", id, name, createdAt)
	require.NoError(t, err)
	return id
}

func widgetWorkspaces(t *testing.T, db *bun.DB) map[string]string {
	t.Helper()
	var rows []struct {
		Name        string `bun:"name"`
		WorkspaceID string `bun:"workspace_id"`
	}
	require.NoError(t, db.NewSelect().Table("widgets").Column("name", "workspace_id").Scan(context.Background(), &rows))
	got := map[string]string{}
	for _, r := range rows {
		got[r.Name] = r.WorkspaceID
	}
	return got
}

func TestAddWorkspaceColumn_FillsEveryRowWithTheOldestWorkspace(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		createWidgets(t, db, "one", "two")
		oldest := insertWorkspace(t, db, "Oldest", "2026-01-01 00:00:00")
		insertWorkspace(t, db, "Newer", "2026-06-01 00:00:00")

		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))

		assert.Equal(t, map[string]string{"one": oldest, "two": oldest}, widgetWorkspaces(t, db))
	})
}

func TestAddWorkspaceColumn_RefusesARowWithNoWorkspace(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		workspace := insertWorkspace(t, db, "Only", "2026-01-01 00:00:00")
		createWidgets(t, db)

		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))

		_, err := db.Exec("INSERT INTO widgets (id, name) VALUES (?, 'nobody')", uuid.NewString())
		assert.Error(t, err, "no workspace in the insert")
		_, err = db.Exec("INSERT INTO widgets (id, name, workspace_id) VALUES (?, 'ghost', ?)", uuid.NewString(), uuid.NewString())
		assert.Error(t, err, "a workspace that doesn't exist")
		_, err = db.Exec("INSERT INTO widgets (id, name, workspace_id) VALUES (?, 'ok', ?)", uuid.NewString(), workspace)
		assert.NoError(t, err)
	})
}

func TestAddWorkspaceColumn_RowsNeedAWorkspaceToGoTo(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		createWidgets(t, db, "orphan")
		err := addWorkspaceColumn(ctx, db, "widgets")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no workspace")
		has, err := hasWorkspaceColumn(ctx, db, "widgets")
		require.NoError(t, err)
		assert.False(t, has, "the table is as it was")
	})
}

func TestAddWorkspaceColumn_AnEmptyTableNeedsNoWorkspace(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		createWidgets(t, db)
		require.NoError(t, addWorkspaceColumn(context.Background(), db, "widgets"))
	})
}

func TestAddWorkspaceColumn_DeletingTheWorkspaceDeletesItsRows(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		createWidgets(t, db, "one")
		doomed := insertWorkspace(t, db, "Doomed", "2026-01-01 00:00:00")
		kept := insertWorkspace(t, db, "Kept", "2026-02-01 00:00:00")
		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))
		_, err := db.Exec("INSERT INTO widgets (id, name, workspace_id) VALUES (?, 'two', ?)", uuid.NewString(), kept)
		require.NoError(t, err)

		_, err = db.Exec("DELETE FROM workspaces WHERE id = ?", doomed)
		require.NoError(t, err)

		assert.Equal(t, map[string]string{"two": kept}, widgetWorkspaces(t, db))
	})
}

func TestAddWorkspaceColumn_ARerunChangesNothing(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		createWidgets(t, db, "one")
		first := insertWorkspace(t, db, "First", "2026-01-01 00:00:00")
		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))
		second := insertWorkspace(t, db, "Second", "2026-02-01 00:00:00")
		_, err := db.Exec("UPDATE widgets SET workspace_id = ?", second)
		require.NoError(t, err)

		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))

		assert.Equal(t, map[string]string{"one": second}, widgetWorkspaces(t, db), "a row a person moved stays; first was %s", first)
		var indexes int
		require.NoError(t, db.NewRaw(indexCount(db), "idx_widgets_workspace_id").Scan(ctx, &indexes))
		assert.Equal(t, 1, indexes)
	})
}

func indexCount(db *bun.DB) string {
	if db.Dialect().Name().String() == "sqlite" {
		return "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?"
	}
	return "SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?"
}

func TestDropWorkspaceColumn_LeavesTheTableAsItWas(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		createWidgets(t, db, "one", "two")
		insertWorkspace(t, db, "First", "2026-01-01 00:00:00")
		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"))

		require.NoError(t, dropWorkspaceColumn(ctx, db, "widgets"))

		has, err := hasWorkspaceColumn(ctx, db, "widgets")
		require.NoError(t, err)
		assert.False(t, has)
		var names []string
		require.NoError(t, db.NewSelect().Table("widgets").Column("name").Order("name").Scan(ctx, &names))
		assert.Equal(t, []string{"one", "two"}, names, "the rows stay")
		var indexes int
		require.NoError(t, db.NewRaw(indexCount(db), "idx_widgets_note").Scan(ctx, &indexes))
		assert.Equal(t, 1, indexes, "the table's other indexes stay")
		require.NoError(t, db.NewRaw(indexCount(db), "idx_widgets_workspace_id").Scan(ctx, &indexes))
		assert.Equal(t, 0, indexes)
		_, err = db.Exec("INSERT INTO widgets (id, name) VALUES (?, 'three')", uuid.NewString())
		assert.NoError(t, err, "the column is not there to refuse it")
		_, err = db.Exec("INSERT INTO widgets (id, name) VALUES (?, 'one')", uuid.NewString())
		assert.Error(t, err, "the unique name stays")

		require.NoError(t, addWorkspaceColumn(ctx, db, "widgets"), "and it can come back")
	})
}

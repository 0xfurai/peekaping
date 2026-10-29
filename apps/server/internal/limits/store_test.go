package limits_test

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
	"peekaping/internal/limits"
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

func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "limits.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "limits_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// forEachBackend runs fn over a migrated SQLite and PostgreSQL database
func forEachBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(context.Background()))
			_, err := migrator.Migrate(context.Background())
			require.NoError(t, err)
			fn(t, db)
		})
	}
}

func TestSQLStore_StoresAWorkspacesOwnLimits(t *testing.T) {
	forEachBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		workspaces := workspace.NewSQLRepository(db)
		first, err := workspaces.Create(ctx, "First")
		require.NoError(t, err)
		second, err := workspaces.Create(ctx, "Second")
		require.NoError(t, err)
		store := limits.NewSQLStore(db)

		own, err := store.AllOwn(ctx)
		require.NoError(t, err)
		assert.Empty(t, own, "a new workspace has none of its own")

		require.NoError(t, store.SetOwn(ctx, first.ID, &limits.Limits{Monitors: ptr(5), ProbesPerMonitor: ptr(2)}))
		require.NoError(t, store.SetOwn(ctx, second.ID, &limits.Limits{}))
		own, err = store.AllOwn(ctx)
		require.NoError(t, err)
		assert.Equal(t, limits.Limits{Monitors: ptr(5), ProbesPerMonitor: ptr(2)}, own[first.ID])
		unlimited, ok := own[second.ID]
		assert.True(t, ok, "no limits is stored as a value")
		assert.True(t, unlimited.IsZero())

		require.NoError(t, store.SetOwn(ctx, first.ID, &limits.Limits{Monitors: ptr(9)}))
		own, err = store.AllOwn(ctx)
		require.NoError(t, err)
		assert.Equal(t, limits.Limits{Monitors: ptr(9)}, own[first.ID], "a second write replaces the first")

		require.NoError(t, store.SetOwn(ctx, first.ID, nil))
		own, err = store.AllOwn(ctx)
		require.NoError(t, err)
		_, ok = own[first.ID]
		assert.False(t, ok, "removed, the workspace has the install's defaults again")
		assert.Contains(t, own, second.ID, "and the other workspace keeps its own")

		chain := limits.NewChain(store, limits.EnvDefaults{Monitors: ptr(10)})
		got, origin, err := chain.For(ctx, first.ID)
		require.NoError(t, err)
		assert.Equal(t, limits.OriginInstall, origin)
		assert.Equal(t, 10, *got.Monitors)
	})
}

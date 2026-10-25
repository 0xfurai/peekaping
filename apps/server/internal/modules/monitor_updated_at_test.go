package modules_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/monitor"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
)

// The stale-monitor watchdog counts a resumed monitor as checked at the moment it was
// resumed, so pausing and resuming must move updated_at on every backend
func TestMonitorPartialUpdateMovesUpdatedAt(t *testing.T) {
	exercise := func(t *testing.T, repo monitor.MonitorRepository) {
		ctx := context.Background()
		created, err := repo.Create(ctx, &monitor.Model{Type: "http", Name: "Monitor", Interval: 60, Timeout: 10, Active: true, Config: "{}"})
		require.NoError(t, err)
		before, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)

		time.Sleep(50 * time.Millisecond)
		active := true
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &monitor.UpdateModel{Active: &active}))

		after, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.True(t, after.UpdatedAt.After(before.UpdatedAt), "updated_at %v should move past %v", after.UpdatedAt, before.UpdatedAt)
	}

	sqlRepo := func(t *testing.T, db *bun.DB) monitor.MonitorRepository {
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		require.NoError(t, migrator.Init(context.Background()))
		_, err := migrator.Migrate(context.Background())
		require.NoError(t, err)
		return monitor.NewSQLRepository(db)
	}

	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "monitor-updated-at.db"))
		require.NoError(t, err)
		exercise(t, sqlRepo(t, bun.NewDB(sqlDB, sqlitedialect.New())))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pp16_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
		require.NoError(t, err)
		t.Cleanup(func() {
			_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
			require.NoError(t, err)
			require.NoError(t, admin.Close())
		})
		connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
		exercise(t, sqlRepo(t, bun.NewDB(sql.OpenDB(connector), pgdialect.New())))
	})
}

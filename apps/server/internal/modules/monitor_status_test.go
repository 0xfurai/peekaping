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
	"peekaping/internal/config"
	"peekaping/internal/modules/monitor"
	"peekaping/internal/modules/shared"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/driver/sqliteshim"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// onEveryMonitorBackend runs exercise against a monitor repository on SQLite, and on PostgreSQL and
// MongoDB when their test variables are set.
func onEveryMonitorBackend(t *testing.T, exercise func(t *testing.T, repo monitor.MonitorRepository)) {
	t.Helper()
	sqlRepo := func(t *testing.T, db *bun.DB) monitor.MonitorRepository {
		t.Cleanup(func() { require.NoError(t, db.Close()) })
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		require.NoError(t, migrator.Init(context.Background()))
		_, err := migrator.Migrate(context.Background())
		require.NoError(t, err)
		return monitor.NewSQLRepository(db)
	}

	t.Run("SQLite", func(t *testing.T) {
		sqlDB, err := sql.Open(sqliteshim.ShimName, filepath.Join(t.TempDir(), "monitor-status.db"))
		require.NoError(t, err)
		exercise(t, sqlRepo(t, bun.NewDB(sqlDB, sqlitedialect.New())))
	})
	t.Run("Postgres", func(t *testing.T) {
		dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
		}
		admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
		schema := "pp189_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	t.Run("Mongo", func(t *testing.T) {
		uri := os.Getenv("PEE_TEST_MONGO_URI")
		if uri == "" {
			t.Skip("set PEE_TEST_MONGO_URI to run MongoDB integration checks")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
		require.NoError(t, err)
		require.NoError(t, client.Ping(ctx, nil))
		cfg := &config.Config{DBName: "pp189_" + primitive.NewObjectID().Hex()}
		t.Cleanup(func() {
			require.NoError(t, client.Database(cfg.DBName).Drop(context.Background()))
			require.NoError(t, client.Disconnect(context.Background()))
		})
		exercise(t, monitor.NewMongoRepository(client, cfg))
	})
}

// The monitor list filters on the stored status, which only Heartbeats set, so a full update of
// a monitor's settings has to leave it alone.
func TestMonitorFullUpdateKeepsStoredStatus(t *testing.T) {
	onEveryMonitorBackend(t, func(t *testing.T, repo monitor.MonitorRepository) {
		ctx := context.Background()
		created, err := repo.Create(ctx, &monitor.Model{Type: "http", Name: "Monitor", Interval: 60, Timeout: 10, Active: true, Config: "{}"})
		require.NoError(t, err)
		status := shared.MonitorStatusMaintenance
		require.NoError(t, repo.UpdatePartial(ctx, created.ID, &monitor.UpdateModel{Status: &status}))

		err = repo.UpdateFull(ctx, created.ID, &monitor.Model{Type: "http", Name: "Renamed", Interval: 60, Timeout: 10, Active: true, Config: "{}", Status: shared.MonitorStatusUp})
		require.NoError(t, err)

		stored, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", stored.Name)
		assert.Equal(t, shared.MonitorStatusMaintenance, stored.Status)
	})
}

func TestMonitorCreateStoresTheGivenStatus(t *testing.T) {
	onEveryMonitorBackend(t, func(t *testing.T, repo monitor.MonitorRepository) {
		ctx := context.Background()

		created, err := repo.Create(ctx, &monitor.Model{Type: "http", Name: "Monitor", Interval: 60, Timeout: 10, Active: true, Config: "{}", Status: shared.MonitorStatusPending})
		require.NoError(t, err)

		assert.Equal(t, shared.MonitorStatusPending, created.Status)
		stored, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, shared.MonitorStatusPending, stored.Status)
	})
}

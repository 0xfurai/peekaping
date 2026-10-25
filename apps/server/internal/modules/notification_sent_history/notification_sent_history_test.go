package notification_sent_history_test

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
	"peekaping/internal/modules/notification_sent_history"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

const certificate = "certificate"

// sqliteDB opens a SQLite database the way the app does.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "history.db")}, zap.NewNop().Sugar())
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
	schema := "history_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	// assert, not require, so a failed close still drops the schema.
	t.Cleanup(func() {
		assert.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		assert.NoError(t, err)
		assert.NoError(t, admin.Close())
	})
	return db
}

// forEachBackend runs fn against a sent-history service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, svc notification_sent_history.Service)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			fn(t, notification_sent_history.NewService(notification_sent_history.NewSQLRepository(db), zap.NewNop().Sugar()))
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

func record(t *testing.T, svc notification_sent_history.Service, notificationType, monitorID string, days int) {
	t.Helper()
	require.NoError(t, svc.RecordNotificationSent(context.Background(), notificationType, monitorID, days))
}

func sent(t *testing.T, svc notification_sent_history.Service, notificationType, monitorID string, days int) bool {
	t.Helper()
	ok, err := svc.CheckIfNotificationSent(context.Background(), notificationType, monitorID, days)
	require.NoError(t, err)
	return ok
}

// history returns the days recorded for monitorID and notificationType.
func history(t *testing.T, svc notification_sent_history.Service, monitorID, notificationType string) []int {
	t.Helper()
	models, err := svc.GetNotificationHistory(context.Background(), monitorID, notificationType)
	require.NoError(t, err)
	days := []int{}
	for _, m := range models {
		days = append(days, m.Days)
	}
	return days
}

func TestRecordNotificationSent_IsIdempotent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc notification_sent_history.Service) {
		monitorID := uuid.NewString()

		record(t, svc, certificate, monitorID, 7)
		record(t, svc, certificate, monitorID, 7)

		assert.Equal(t, []int{7}, history(t, svc, monitorID, certificate))
	})
}

func TestCheckIfNotificationSent_ThresholdOrANearerOne(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc notification_sent_history.Service) {
		monitorID, otherMonitorID := uuid.NewString(), uuid.NewString()

		record(t, svc, certificate, monitorID, 21)
		assert.True(t, sent(t, svc, certificate, monitorID, 21))
		assert.False(t, sent(t, svc, certificate, monitorID, 14), "14 is still due after 21")
		assert.False(t, sent(t, svc, certificate, monitorID, 7), "7 is still due after 21")
		assert.False(t, sent(t, svc, certificate, otherMonitorID, 21), "another monitor has its own history")
		assert.False(t, sent(t, svc, "monitor", monitorID, 21), "another type has its own history")

		record(t, svc, certificate, monitorID, 7)
		assert.True(t, sent(t, svc, certificate, monitorID, 14), "7 covers the farther 14")
		assert.True(t, sent(t, svc, certificate, monitorID, 7))
	})
}

func TestClearNotificationHistory_ClearsOnlyThatMonitorAndType(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc notification_sent_history.Service) {
		monitorID, otherMonitorID := uuid.NewString(), uuid.NewString()
		record(t, svc, certificate, monitorID, 7)
		record(t, svc, certificate, monitorID, 14)
		record(t, svc, certificate, otherMonitorID, 7)
		record(t, svc, "monitor", monitorID, 7)

		require.NoError(t, svc.ClearNotificationHistory(context.Background(), monitorID, certificate))

		assert.Empty(t, history(t, svc, monitorID, certificate))
		assert.Equal(t, []int{7}, history(t, svc, otherMonitorID, certificate))
		assert.Equal(t, []int{7}, history(t, svc, monitorID, "monitor"))
	})
}

func TestCleanupOldRecords_KeepsRecentRows(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc notification_sent_history.Service) {
		monitorID := uuid.NewString()
		record(t, svc, certificate, monitorID, 7)

		require.NoError(t, svc.CleanupOldRecords(context.Background(), 90))

		assert.Equal(t, []int{7}, history(t, svc, monitorID, certificate))
	})
}

// A cutoff a day in the future makes every row old, which tests the delete
// without backdating rows.
func TestCleanupOldRecords_RemovesRowsCreatedBeforeTheCutoff(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc notification_sent_history.Service) {
		monitorID := uuid.NewString()
		record(t, svc, certificate, monitorID, 7)

		require.NoError(t, svc.CleanupOldRecords(context.Background(), -1))

		assert.Empty(t, history(t, svc, monitorID, certificate))
	})
}

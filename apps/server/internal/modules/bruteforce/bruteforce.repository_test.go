package bruteforce_test

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
	"peekaping/internal/modules/bruteforce"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

const (
	maxAttempts = 3
	window      = time.Minute
	lockout     = 5 * time.Minute

	key      = "192.0.2.1:admin@example.com"
	otherKey = "192.0.2.1:other@example.com"
)

// sqliteDB opens a SQLite database the way the app does, with one connection.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "lockout.db")}, zap.NewNop().Sugar())
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
	schema := "lockout_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// forEachBackend runs fn against a lockout service on every database backend.
func forEachBackend(t *testing.T, fn func(t *testing.T, svc bruteforce.Service)) {
	sqlBackend := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx := context.Background()
			migrator := migrate.NewMigrator(db, migrations.Migrations)
			require.NoError(t, migrator.Init(ctx))
			_, err := migrator.Migrate(ctx)
			require.NoError(t, err)
			fn(t, bruteforce.NewService(bruteforce.NewSQLRepository(db), zap.NewNop().Sugar()))
		}
	}
	t.Run("SQLite", sqlBackend(sqliteDB))
	t.Run("Postgres", sqlBackend(postgresDB))
}

// fail records n failed logins for k at now and returns the last result.
func fail(t *testing.T, svc bruteforce.Service, k string, now time.Time, n int) (bool, time.Time) {
	t.Helper()
	var locked bool
	var until time.Time
	for range n {
		var err error
		locked, until, err = svc.OnFailure(context.Background(), k, now, window, maxAttempts, lockout)
		require.NoError(t, err)
	}
	return locked, until
}

func isLocked(t *testing.T, svc bruteforce.Service, k string) (bool, time.Time) {
	t.Helper()
	locked, until, err := svc.IsLocked(context.Background(), k)
	require.NoError(t, err)
	return locked, until
}

func TestLockout_LocksAKeyAfterMaxFailuresInTheWindow(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc bruteforce.Service) {
		now := time.Now()

		locked, _ := fail(t, svc, key, now, maxAttempts-1)
		assert.False(t, locked, "one failure short of the limit")
		locked, _ = isLocked(t, svc, key)
		assert.False(t, locked)

		locked, until := fail(t, svc, key, now, 1)
		assert.True(t, locked, "the failure that reaches the limit locks")
		assert.WithinDuration(t, now.Add(lockout), until, time.Millisecond)

		locked, until = isLocked(t, svc, key)
		assert.True(t, locked)
		assert.WithinDuration(t, now.Add(lockout), until, time.Millisecond)

		locked, _ = fail(t, svc, otherKey, now, maxAttempts-1)
		assert.False(t, locked, "another account from the same IP keeps its own count")
		locked, _ = isLocked(t, svc, otherKey)
		assert.False(t, locked)
	})
}

func TestLockout_ALimitOfOneLocksOnTheFirstFailure(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc bruteforce.Service) {
		now := time.Now()

		locked, until, err := svc.OnFailure(context.Background(), key, now, window, 1, lockout)
		require.NoError(t, err)
		assert.True(t, locked)
		assert.WithinDuration(t, now.Add(lockout), until, time.Millisecond)
		locked, _ = isLocked(t, svc, key)
		assert.True(t, locked)
	})
}

func TestLockout_ResetClearsFailuresAndLock(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc bruteforce.Service) {
		ctx := context.Background()
		now := time.Now()

		fail(t, svc, key, now, maxAttempts-1)
		require.NoError(t, svc.Reset(ctx, key))
		locked, _ := fail(t, svc, key, now, maxAttempts-1)
		assert.False(t, locked, "failures before a successful login don't count")

		locked, _ = fail(t, svc, key, now, 1)
		require.True(t, locked)
		require.NoError(t, svc.Reset(ctx, key))
		locked, _ = isLocked(t, svc, key)
		assert.False(t, locked, "a reset lifts the lock")
	})
}

func TestLockout_FailuresOutsideTheWindowStartOver(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc bruteforce.Service) {
		start := time.Now().Add(-2 * window)
		later := start.Add(window + time.Second)

		fail(t, svc, key, start, maxAttempts-1)
		locked, _ := fail(t, svc, key, later, maxAttempts-1)
		assert.False(t, locked, "the count restarted at the first failure past the window")

		locked, _ = fail(t, svc, key, later, 1)
		assert.True(t, locked)
	})
}

func TestLockout_LockEndsWhenItsTimePasses(t *testing.T) {
	forEachBackend(t, func(t *testing.T, svc bruteforce.Service) {
		past := time.Now().Add(-lockout - time.Second)

		locked, _ := fail(t, svc, key, past, maxAttempts)
		require.True(t, locked)

		locked, _ = isLocked(t, svc, key)
		assert.False(t, locked)
	})
}

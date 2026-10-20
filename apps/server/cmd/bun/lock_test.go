package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v2"
)

// How long a test lets a migrate wait for the lock. Long enough for a slow machine to finish
// the migrations, short enough that a hang fails the test instead of stalling it.
const testLockWait = 30 * time.Second

func sqliteConfig(t *testing.T) *config.DBConfig {
	t.Helper()
	return &config.DBConfig{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "peekaping.db")}
}

// postgresConfig creates a database of its own on the server PEE_TEST_POSTGRES_DSN names, so the
// advisory lock, which belongs to a database, is the test's alone.
func postgresConfig(t *testing.T) *config.DBConfig {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run PostgreSQL integration checks")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := u.User.Password()
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	name := "pp226_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec("CREATE DATABASE ?", bun.Ident(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec("DROP DATABASE ? WITH (FORCE)", bun.Ident(name))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return &config.DBConfig{DBType: "postgres", DBHost: u.Hostname(), DBPort: u.Port(), DBUser: u.User.Username(), DBPass: password, DBName: name}
}

// eachBackend runs the test on every storage backend that has a migrate lock to speak of. Each
// run gets a new, empty database.
func eachBackend(t *testing.T, test func(t *testing.T, cfg *config.DBConfig)) {
	t.Helper()
	for _, backend := range []struct {
		name   string
		config func(*testing.T) *config.DBConfig
	}{{"SQLite", sqliteConfig}, {"PostgreSQL", postgresConfig}} {
		t.Run(backend.name, func(t *testing.T) { test(t, backend.config(t)) })
	}
}

// connect opens a database connection the way `bun db` does. Each call is another pool, the
// way each migrate process has its own.
func connect(t *testing.T, cfg *config.DBConfig) *bun.DB {
	t.Helper()
	db, err := connectToDatabase(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// initialized is `bun db init`: the database with its migration tables, nothing migrated.
func initialized(t *testing.T, cfg *config.DBConfig) *bun.DB {
	t.Helper()
	db := connect(t, cfg)
	require.NoError(t, migrate.NewMigrator(db, migrations.Migrations).Init(context.Background()))
	return db
}

// unapplied counts the migrations that haven't run.
func unapplied(t *testing.T, db *bun.DB) int {
	t.Helper()
	ms, err := migrate.NewMigrator(db, migrations.Migrations).MigrationsWithStatus(context.Background())
	require.NoError(t, err)
	return len(ms.Unapplied())
}

func allMigrations() int { return len(migrations.Migrations.Sorted()) }

// lockRows counts the rows in bun's own lock table, which `bun db lock` writes.
func lockRows(t *testing.T, db *bun.DB) int {
	t.Helper()
	n, err := db.NewSelect().TableExpr("bun_migration_locks").Count(context.Background())
	require.NoError(t, err)
	return n
}

// lockHolder is a migrate that holds the advisory lock, on a connection of its own.
type lockHolder struct{ db *bun.DB }

func holdMigrationLock(t *testing.T, cfg *config.DBConfig) *lockHolder {
	t.Helper()
	db := connect(t, cfg)
	// The one connection is the session that holds the lock; it stays in the pool when idle.
	db.SetMaxOpenConns(1)
	_, err := db.Exec(fmt.Sprintf("SELECT pg_advisory_lock(%d)", migrationLockKey))
	require.NoError(t, err)
	require.True(t, advisoryLockHeld(t, cfg), "the holder doesn't hold the lock")
	return &lockHolder{db}
}

// release unlocks, as a migrate that finishes does.
func (h *lockHolder) release(t *testing.T) {
	t.Helper()
	_, err := h.db.Exec(fmt.Sprintf("SELECT pg_advisory_unlock(%d)", migrationLockKey))
	require.NoError(t, err)
	require.NoError(t, h.db.Close())
}

// kill drops the connection with the lock still held, as the death of a migrate does.
func (h *lockHolder) kill(t *testing.T) {
	t.Helper()
	require.NoError(t, h.db.Close())
}

// advisoryLockHeld asks from a new connection whether somebody holds the migrate lock.
func advisoryLockHeld(t *testing.T, cfg *config.DBConfig) bool {
	t.Helper()
	db := connect(t, cfg)
	db.SetMaxOpenConns(1)
	var got bool
	require.NoError(t, db.NewRaw(fmt.Sprintf("SELECT pg_try_advisory_lock(%d)", migrationLockKey)).Scan(context.Background(), &got))
	if got {
		_, err := db.Exec(fmt.Sprintf("SELECT pg_advisory_unlock(%d)", migrationLockKey))
		require.NoError(t, err)
	}
	return !got
}

// migratesWaiting counts the sessions waiting for an advisory lock.
func migratesWaiting(db *bun.DB) int {
	var n int
	err := db.NewRaw("SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted").Scan(context.Background(), &n)
	if err != nil {
		return 0
	}
	return n
}

// A migrate that was killed while it held bun's lock left a row in bun_migration_locks, and every
// later migrate failed with "migrations table is already locked". PostgreSQL and SQLite don't
// lock on that row any more, so a row an older version left behind is ignored.
func TestMigrateIgnoresALockRowLeftByAnOlderVersion(t *testing.T) {
	eachBackend(t, func(t *testing.T, cfg *config.DBConfig) {
		ctx := context.Background()
		db := initialized(t, cfg)
		require.NoError(t, migrate.NewMigrator(db, migrations.Migrations).Lock(ctx)) // the killed migrate's lock
		require.Equal(t, 1, lockRows(t, db))

		require.NoError(t, migrateUp(ctx, db, testLockWait))

		assert.Zero(t, unapplied(t, db))
		assert.Equal(t, 1, lockRows(t, db), "migrate took or released a row in bun_migration_locks")
	})
}

func TestRollbackIgnoresALockRowLeftByAnOlderVersion(t *testing.T) {
	eachBackend(t, func(t *testing.T, cfg *config.DBConfig) {
		ctx := context.Background()
		db := initialized(t, cfg)
		require.NoError(t, migrate.NewMigrator(db, migrations.Migrations).Lock(ctx))

		// Nothing is migrated, so there is nothing to roll back: the lock is all this checks.
		require.NoError(t, migrateDown(ctx, db, testLockWait))

		assert.Equal(t, 1, lockRows(t, db), "rollback took or released a row in bun_migration_locks")
	})
}

// A second migrate that starts while the first runs waits for it, then applies what is left,
// which is usually nothing. When the first one dies instead, its connection goes with it, and
// PostgreSQL drops its lock: the second one doesn't wait for anyone to clean up.
func TestMigrateWaitsForTheLockHolder(t *testing.T) {
	for name, end := range map[string]func(*lockHolder, *testing.T){
		"finishes":  (*lockHolder).release,
		"is killed": (*lockHolder).kill,
	} {
		t.Run("holder "+name, func(t *testing.T) {
			cfg := postgresConfig(t)
			db := initialized(t, cfg)
			holder := holdMigrationLock(t, cfg)
			second := connect(t, cfg)
			done := make(chan error, 1)
			go func() { done <- migrateUp(context.Background(), second, testLockWait) }()

			require.Eventually(t, func() bool { return migratesWaiting(db) == 1 }, 10*time.Second, 20*time.Millisecond, "migrate didn't wait for the lock")
			select {
			case err := <-done:
				t.Fatalf("migrate finished while the lock was held: %v", err)
			default:
			}
			assert.Equal(t, allMigrations(), unapplied(t, db), "migrate ran while the lock was held")

			end(holder, t)

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(testLockWait):
				t.Fatal("migrate didn't continue after the lock holder was gone")
			}
			assert.Zero(t, unapplied(t, db))
			assert.False(t, advisoryLockHeld(t, cfg), "migrate left the lock held")
		})
	}
}

func TestConcurrentMigratesRunOneAfterTheOther(t *testing.T) {
	cfg := postgresConfig(t)
	db := initialized(t, cfg)
	const processes = 4
	start := make(chan struct{})
	results := make(chan error, processes)
	for range processes {
		process := connect(t, cfg)
		go func() {
			<-start
			results <- migrateUp(context.Background(), process, testLockWait)
		}()
	}
	close(start)
	for range processes {
		assert.NoError(t, <-results)
	}

	assert.Zero(t, unapplied(t, db))
	assert.False(t, advisoryLockHeld(t, cfg), "the migrates left the lock held")
	// The first applied everything as one group; the others found nothing left to run.
	var groups int
	require.NoError(t, db.NewSelect().TableExpr("bun_migrations").ColumnExpr("count(DISTINCT group_id)").Scan(context.Background(), &groups))
	assert.Equal(t, 1, groups)
}

func TestMigrateAndRollbackGiveUpWhenTheLockStaysHeld(t *testing.T) {
	cfg := postgresConfig(t)
	ctx := context.Background()
	db := initialized(t, cfg)
	holder := holdMigrationLock(t, cfg)

	started := time.Now()
	err := migrateUp(ctx, db, time.Second)
	require.ErrorContains(t, err, "another migration still holds the database lock after 1s")
	assert.GreaterOrEqual(t, time.Since(started), time.Second)
	assert.Equal(t, allMigrations(), unapplied(t, db), "migrate ran without the lock")

	holder.release(t)
	require.NoError(t, migrateUp(ctx, db, testLockWait))
	holder = holdMigrationLock(t, cfg)

	err = migrateDown(ctx, db, time.Second)
	require.ErrorContains(t, err, "another migration still holds the database lock after 1s")
	assert.Zero(t, unapplied(t, db), "rollback ran without the lock")

	holder.release(t)
	require.NoError(t, migrateDown(ctx, db, testLockWait))
	assert.Equal(t, allMigrations(), unapplied(t, db), "the last group wasn't rolled back")
}

// `bun db lock` and `unlock` stay what they were: they take and clear bun's row, for anyone who
// needs to stop migrations by hand.
func TestLockAndUnlockCommandsUseTheLocksTable(t *testing.T) {
	cfg := sqliteConfig(t)
	setDatabaseEnv(t, cfg)
	db := connect(t, cfg)
	run := func(args ...string) error {
		app := &cli.App{Commands: []*cli.Command{newDBCommand()}}
		return app.Run(append([]string{"bun", "db"}, args...))
	}
	require.NoError(t, run("init"))

	require.NoError(t, run("lock"))
	assert.Equal(t, 1, lockRows(t, db))
	assert.ErrorContains(t, run("lock"), "migrations table is already locked")

	require.NoError(t, run("unlock"))
	assert.Equal(t, 0, lockRows(t, db))
}

// How long the command waits for the lock: --lock-timeout, or MIGRATE_LOCK_TIMEOUT when the flag
// isn't given. Both commands that take the lock accept it.
func TestLockTimeoutFlagAndEnvironment(t *testing.T) {
	cfg := postgresConfig(t)
	setDatabaseEnv(t, cfg)
	initialized(t, cfg)
	holder := holdMigrationLock(t, cfg)
	defer holder.release(t)
	run := func(args ...string) error {
		app := &cli.App{Commands: []*cli.Command{newDBCommand()}}
		return app.Run(append([]string{"bun", "db"}, args...))
	}

	assert.ErrorContains(t, run("migrate", "--lock-timeout", "300ms"), "lock after 300ms")
	assert.ErrorContains(t, run("rollback", "--lock-timeout", "300ms"), "lock after 300ms")

	t.Setenv("MIGRATE_LOCK_TIMEOUT", "500ms")
	assert.ErrorContains(t, run("migrate"), "lock after 500ms")
	assert.ErrorContains(t, run("migrate", "--lock-timeout", "300ms"), "lock after 300ms", "the flag wins over the environment")
	assert.ErrorContains(t, run("migrate", "--lock-timeout", "0s"), "--lock-timeout must be greater than zero")
}

// setDatabaseEnv points `bun db` at the database, the way run-migrations.sh's environment does.
func setDatabaseEnv(t *testing.T, cfg *config.DBConfig) {
	t.Helper()
	t.Setenv("DB_TYPE", cfg.DBType)
	t.Setenv("DB_HOST", cfg.DBHost)
	t.Setenv("DB_PORT", cfg.DBPort)
	t.Setenv("DB_NAME", cfg.DBName)
	t.Setenv("DB_USER", cfg.DBUser)
	t.Setenv("DB_PASS", cfg.DBPass)
}

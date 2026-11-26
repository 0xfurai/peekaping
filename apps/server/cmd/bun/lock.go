package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v2"
)

// migrationLockKey is the key of the PostgreSQL advisory lock that keeps two migrates apart.
// Every migrate, of every version, must ask for this same key, so it is fixed: don't change it.
// It is the ASCII bytes of "peekmig".
const migrationLockKey int64 = 0x7065656b6d6967

// defaultLockTimeout is how long a migrate waits for another one to finish. It is long enough
// for a slow migration, such as a backfill, and short enough that a stuck one shows up as a
// failed start instead of a hang.
const defaultLockTimeout = 10 * time.Minute

// lockTimeoutFlag is how long `migrate` and `rollback` wait for another migration's lock, set
// with --lock-timeout or MIGRATE_LOCK_TIMEOUT.
func lockTimeoutFlag() *cli.DurationFlag {
	return &cli.DurationFlag{
		Name:    "lock-timeout",
		Usage:   "how long to wait for another migration to finish before giving up (PostgreSQL)",
		EnvVars: []string{"MIGRATE_LOCK_TIMEOUT"},
		Value:   defaultLockTimeout,
	}
}

func lockTimeout(c *cli.Context) (time.Duration, error) {
	wait := c.Duration("lock-timeout")
	if wait <= 0 {
		return 0, fmt.Errorf("--lock-timeout must be greater than zero, got %s", wait)
	}
	return wait, nil
}

// lockMigrations takes the lock a migrate holds while it changes the database, and returns what
// releases it. The lock depends on the database:
//
//   - PostgreSQL: a session advisory lock on a connection of its own. PostgreSQL drops it when
//     that connection closes, so a migrate that is killed can't leave it behind. A migrate that
//     finds it taken waits up to wait for the one holding it to finish.
//   - SQLite: none. One process migrates at startup.
//   - Anything else (MySQL): bun's lock, a row in bun_migration_locks. A migrate that is killed
//     leaves the row behind; `bun db unlock` clears it.
func lockMigrations(ctx context.Context, migrator *migrate.Migrator, wait time.Duration) (unlock func(context.Context) error, err error) {
	db := migrator.DB()
	switch db.Dialect().Name() {
	case dialect.PG:
		return lockPostgres(ctx, db, wait)
	case dialect.SQLite:
		return func(context.Context) error { return nil }, nil
	default:
		if err := migrator.Lock(ctx); err != nil {
			return nil, err
		}
		return migrator.Unlock, nil
	}
}

func lockPostgres(ctx context.Context, db *bun.DB, wait time.Duration) (func(context.Context) error, error) {
	// An advisory lock belongs to a session, so take it on one connection and keep that connection
	// out of the pool until the migrate is done.
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: failed to connect for the database lock: %w", err)
	}
	// Closing the connection ends the session, and PostgreSQL drops the session's locks with it.
	// database/sql would put a plain Close back in the pool, so mark the connection bad instead.
	end := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }

	// lock_timeout ends the wait for the lock after wait.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET lock_timeout = %d", max(wait.Milliseconds(), 1))); err != nil {
		end()
		return nil, fmt.Errorf("migrate: failed to set the lock timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SELECT pg_advisory_lock(%d)", migrationLockKey)); err != nil {
		end()
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "55P03" { // lock_not_available
			return nil, fmt.Errorf("migrate: another migration still holds the database lock after %s; wait for it, or raise --lock-timeout (%w)", wait, err)
		}
		return nil, fmt.Errorf("migrate: failed to take the database lock: %w", err)
	}

	return func(ctx context.Context) error {
		defer end()
		// Unlock before the connection closes. If that fails, closing it still drops the lock.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("SELECT pg_advisory_unlock(%d)", migrationLockKey)); err != nil {
			return fmt.Errorf("migrate: failed to release the database lock: %w", err)
		}
		return nil
	}, nil
}

package main

import (
	"context"
	"fmt"
	"peekaping/cmd/bun/migrations"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// migrateUp applies the migrations that haven't run yet. If another migrate holds the lock, it
// waits up to lockWait for it.
func migrateUp(ctx context.Context, db *bun.DB, lockWait time.Duration) error {
	// Mark a migration applied only once it succeeds, so one that fails
	// or is killed halfway runs again on the next migrate.
	migrator := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
	unlock, err := lockMigrations(ctx, migrator, lockWait)
	if err != nil {
		return err
	}
	defer unlock(ctx) // nolint:errcheck

	// No rollback on failure: the failed migration isn't recorded, so
	// Rollback would undo the last recorded group, possibly an earlier
	// release. The migrations before it stay applied; fix and rerun.
	group, err := migrator.Migrate(ctx)
	if err != nil {
		return err
	}
	if group.IsZero() {
		fmt.Printf("there are no new migrations to run (database is up to date)\n")
		return nil
	}
	fmt.Printf("migrated to %s\n", group)
	return nil
}

// migrateDown rolls back the last migration group. It takes the same lock as migrateUp.
func migrateDown(ctx context.Context, db *bun.DB, lockWait time.Duration) error {
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	unlock, err := lockMigrations(ctx, migrator, lockWait)
	if err != nil {
		return err
	}
	defer unlock(ctx) //nolint:errcheck

	group, err := migrator.Rollback(ctx)
	if err != nil {
		return err
	}
	if group.IsZero() {
		fmt.Printf("there are no groups to roll back\n")
		return nil
	}
	fmt.Printf("rolled back %s\n", group)
	return nil
}

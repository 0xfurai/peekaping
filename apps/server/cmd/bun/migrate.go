package main

import (
	"context"
	"fmt"
	"peekaping/cmd/bun/migrations"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// migrateUp applies the migrations that haven't run yet.
func migrateUp(ctx context.Context, db *bun.DB) error {
	// Mark a migration applied only once it succeeds, so one that fails
	// or is killed halfway runs again on the next migrate.
	migrator := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
	if err := migrator.Lock(ctx); err != nil {
		return err
	}
	defer migrator.Unlock(ctx) // nolint:errcheck

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

// migrateDown rolls back the last migration group.
func migrateDown(ctx context.Context, db *bun.DB) error {
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	if err := migrator.Lock(ctx); err != nil {
		return err
	}
	defer migrator.Unlock(ctx) //nolint:errcheck

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

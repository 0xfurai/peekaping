package heartbeat_test

import (
	"context"
	"testing"

	"peekaping/cmd/bun/migrations"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/migrate"
)

// addHeartbeatInstanceMigration is the migration that added instance_id to heartbeats (PP-331).
const addHeartbeatInstanceMigration = "20261030120000"

// heartbeatColumn reports whether heartbeats has the column, and if so whether it allows NULL and
// what its default is.
func heartbeatColumn(t *testing.T, db *bun.DB, name string) (exists, nullable bool, def string) {
	t.Helper()
	ctx := context.Background()
	if db.Dialect().Name() == dialect.PG {
		var nullability string
		var dflt *string
		err := db.QueryRowContext(ctx, `SELECT is_nullable, column_default FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = 'heartbeats' AND column_name = ?`, name).Scan(&nullability, &dflt)
		if err != nil {
			return false, false, ""
		}
		if dflt != nil {
			def = *dflt
		}
		return true, nullability == "YES", def
	}
	rows, err := db.QueryContext(ctx, `SELECT name, "notnull", dflt_value FROM pragma_table_info('heartbeats')`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var col string
		var notNull int
		var dflt *string
		require.NoError(t, rows.Scan(&col, &notNull, &dflt))
		if col == name {
			if dflt != nil {
				def = *dflt
			}
			return true, notNull == 0, def
		}
	}
	require.NoError(t, rows.Err())
	return false, false, ""
}

// AC5: the migration adds a nullable column with no default (so neither database rewrites the
// table), leaves the heartbeats that exist with no instance, and rolls back.
func TestMigrations_HeartbeatInstanceIDGoesDownAndComesBack(t *testing.T) {
	for _, backend := range []struct {
		name string
		open func(*testing.T) *bun.DB
	}{{"SQLite", sqliteDB}, {"Postgres", postgresDB}} {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			db := backend.open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addHeartbeatInstanceMigration {
					before.Add(m)
				}
			}
			old := migrate.NewMigrator(db, before)
			require.NoError(t, old.Init(ctx))
			_, err := old.Migrate(ctx)
			require.NoError(t, err)
			exists, _, _ := heartbeatColumn(t, db, "instance_id")
			require.False(t, exists, "no column before the migration")
			monitorID, heartbeatID := insertMonitor(t, db), uuid.NewString()
			_, err = db.Exec(`INSERT INTO heartbeats (id, monitor_id, status, msg, ping, time) VALUES (?, ?, 1, 'before', 7, CURRENT_TIMESTAMP)`, heartbeatID, monitorID)
			require.NoError(t, err)

			upgrade := migrate.NewMigrator(db, migrations.Migrations)
			_, err = upgrade.Migrate(ctx)
			require.NoError(t, err)

			exists, nullable, def := heartbeatColumn(t, db, "instance_id")
			assert.True(t, exists, "the column is added")
			assert.True(t, nullable, "nullable")
			assert.Empty(t, def, "with no default")
			var instance *string
			require.NoError(t, db.QueryRow(`SELECT instance_id FROM heartbeats WHERE id = ?`, heartbeatID).Scan(&instance))
			assert.Nil(t, instance, "a heartbeat that existed has no instance")
			_, err = db.Exec(`INSERT INTO heartbeats (id, monitor_id, status, msg, ping, time) VALUES (?, ?, 1, 'after', 7, CURRENT_TIMESTAMP)`, uuid.NewString(), monitorID)
			require.NoError(t, err, "a writer that doesn't know the column still inserts")

			_, err = upgrade.Rollback(ctx)
			require.NoError(t, err)
			exists, _, _ = heartbeatColumn(t, db, "instance_id")
			assert.False(t, exists, "the last group of migrations is this one")
			count, err := db.NewSelect().Table("heartbeats").Count(ctx)
			require.NoError(t, err)
			assert.Equal(t, 2, count, "the heartbeats stay")

			_, err = upgrade.Migrate(ctx)
			require.NoError(t, err)
			exists, nullable, _ = heartbeatColumn(t, db, "instance_id")
			assert.True(t, exists && nullable, "it comes back")
		})
	}
}

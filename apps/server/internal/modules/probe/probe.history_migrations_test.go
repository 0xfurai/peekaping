package probe_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/probe"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/migrate"
)

// firstHistoryMigration is the first migration of the probe history (PP-330)
const firstHistoryMigration = "20261028120000"

// migratedBeforeHistory migrates the database as it was before the probe history, and returns the
// migrator that has every migration, to upgrade it with
func migratedBeforeHistory(t *testing.T, db *bun.DB) *migrate.Migrator {
	t.Helper()
	ctx := context.Background()
	before := migrate.NewMigrations()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name < firstHistoryMigration {
			before.Add(m)
		}
	}
	old := migrate.NewMigrator(db, before)
	require.NoError(t, old.Init(ctx))
	_, err := old.Migrate(ctx)
	require.NoError(t, err)
	return migrate.NewMigrator(db, migrations.Migrations)
}

func forEachUnmigratedBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) { fn(t, open(t)) })
	}
}

func TestMigrations_StartTheHistoryOfEveryExistingProbeWithItsStatus(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBeforeHistory(t, db)
		onlineID, offlineID := uuid.NewString(), uuid.NewString()
		for _, p := range []struct{ id, name, status string }{{onlineID, "Frankfurt", "online"}, {offlineID, "Paris", "offline"}} {
			_, err := db.Exec("INSERT INTO probes (id, name, token_hash, status) VALUES (?, ?, ?, ?)", p.id, p.name, "sha256$"+p.name, p.status)
			require.NoError(t, err)
		}
		_, err := db.Exec("UPDATE probes SET status = 'online' WHERE id = 'local'")
		require.NoError(t, err)
		before := time.Now().UTC().Add(-time.Second)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		repo := probe.NewSQLRepository(db)
		for id, want := range map[string]probe.Status{onlineID: probe.StatusOnline, offlineID: probe.StatusOffline, "local": probe.StatusOnline} {
			changes, err := repo.FindStatusChanges(ctx, id, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			require.Len(t, changes, 1, "one row for %s", id)
			assert.Equal(t, id, changes[0].ProbeID)
			assert.Equal(t, want, changes[0].Status, "the status the probe had at the upgrade")
			assert.True(t, changes[0].At.After(before), "from the time of the upgrade")
			assert.WithinDuration(t, time.Now(), changes[0].At, time.Minute)
		}

		// The history carries on from the seeded row
		changed, err := repo.UpdateStatus(ctx, onlineID, probe.StatusOnline, probe.StatusOffline, time.Now())
		require.NoError(t, err)
		require.True(t, changed)
		changes, err := repo.FindStatusChanges(ctx, onlineID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.Len(t, changes, 2)
		assert.Equal(t, probe.StatusOffline, changes[1].Status)
	})
}

func TestMigrations_ProbeHistoryTablesGoDownAndComeBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBeforeHistory(t, db)
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		tables := func() int {
			query := db.NewSelect().Table("sqlite_master").Where("name IN ('probe_status_changes', 'probe_instance_connections')")
			if db.Dialect().Name() == dialect.PG {
				query = db.NewSelect().Table("information_schema.tables").
					Where("table_schema = current_schema()").
					Where("table_name IN ('probe_status_changes', 'probe_instance_connections')")
			}
			n, err := query.Count(ctx)
			require.NoError(t, err)
			return n
		}
		require.Equal(t, 2, tables())

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		assert.Equal(t, 0, tables(), "the last group of migrations is the history's")
		probes, err := db.NewSelect().Table("probes").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, probes, "the probes stay")

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		assert.Equal(t, 2, tables())
		changes, err := probe.NewSQLRepository(db).FindStatusChanges(ctx, "local", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		assert.Len(t, changes, 1, "the local probe is seeded again")
	})
}

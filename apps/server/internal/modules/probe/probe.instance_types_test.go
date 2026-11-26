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
	"github.com/uptrace/bun/migrate"
)

// addInstanceTypesMigration is the migration that gave each probe instance its own supported types
const addInstanceTypesMigration = "20261027120000"

// migrateWith migrates the database with the given migrations only
func migrateWith(t *testing.T, db *bun.DB, ms *migrate.Migrations) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, ms)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
}

func typesOf(t *testing.T, repo probe.Repository, probeID string) map[string][]string {
	t.Helper()
	instances, err := repo.FindInstances(context.Background(), probeID)
	require.NoError(t, err)
	types := map[string][]string{}
	for _, instance := range instances {
		types[instance.ID] = instance.SupportedTypes
	}
	return types
}

func TestSQLRepository_AnInstanceKeepsItsOwnSupportedTypes(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
		full := []string{"docker", "http", "tcp"}
		slim := []string{"http", "tcp"}

		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "full", SupportedTypes: full, LastSeenAt: seen}))
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "slim", SupportedTypes: slim, LastSeenAt: seen}))
		require.NoError(t, repo.UpsertInstance(ctx, other.ID, &probe.Instance{ID: "other", SupportedTypes: []string{"dns"}, LastSeenAt: seen}))

		assert.Equal(t, map[string][]string{"full": full, "slim": slim}, typesOf(t, repo, owner.ID), "each instance reads back its own list")
		assert.Equal(t, map[string][]string{"other": {"dns"}}, typesOf(t, repo, other.ID))

		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "full", SupportedTypes: []string{"ping"}, LastSeenAt: seen.Add(time.Minute)}))
		assert.Equal(t, map[string][]string{"full": {"ping"}, "slim": slim}, typesOf(t, repo, owner.ID),
			"a hello replaces its own instance's list and leaves its sibling's")

		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "full", SupportedTypes: nil, LastSeenAt: seen.Add(2 * time.Minute)}))
		assert.Equal(t, map[string][]string{"full": {}, "slim": slim}, typesOf(t, repo, owner.ID), "no types is an empty list, which means every type")

		stored, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{}, stored.SupportedTypes, "an instance's hello does not touch the probe's own list")
	})
}

func TestSQLRepository_FindInstanceReadsOneOfTheProbesInstances(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "slim", Hostname: "edge-1", SupportedTypes: []string{"http"}, LastSeenAt: seen}))

		found, err := repo.FindInstance(ctx, owner.ID, "slim")

		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "slim", found.ID)
		assert.Equal(t, "edge-1", found.Hostname)
		assert.Equal(t, []string{"http"}, found.SupportedTypes)
		missing, err := repo.FindInstance(ctx, owner.ID, "nobody")
		require.NoError(t, err)
		assert.Nil(t, missing)
		foreign, err := repo.FindInstance(ctx, other.ID, "slim")
		require.NoError(t, err)
		assert.Nil(t, foreign, "another probe's instance is out of reach")
	})
}

// An instance that was there before the column was takes the types of its probe, which is what
// every one of them ran as then: the probe's list was the last hello's.
func TestSQLRepository_ExistingInstancesTakeTheirProbesTypes(t *testing.T) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := open(t)
			before := migrate.NewMigrations()
			for _, m := range migrations.Migrations.Sorted() {
				if m.Name < addInstanceTypesMigration {
					before.Add(m)
				}
			}
			migrateWith(t, db, before)
			seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
			addProbe := func(id, types string) {
				_, err := db.Exec(`INSERT INTO probes (id, name, supported_types) VALUES (?, ?, ?)`, id, id, types)
				require.NoError(t, err)
			}
			addInstance := func(probeID, id string) {
				_, err := db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES (?, ?, 'host', 'linux', 'amd64', '1.0.0', 1, ?)`, id, probeID, seen)
				require.NoError(t, err)
			}
			addProbe("frankfurt", `["http","tcp"]`)
			addProbe("paris", `[]`)
			addProbe("lyon", `["dns"]`)
			addInstance("frankfurt", "frankfurt-1")
			addInstance("frankfurt", "frankfurt-2")
			addInstance("paris", "paris-1")

			migrateWith(t, db, migrations.Migrations)

			repo := probe.NewSQLRepository(db)
			assert.Equal(t, map[string][]string{"frankfurt-1": {"http", "tcp"}, "frankfurt-2": {"http", "tcp"}}, typesOf(t, repo, "frankfurt"))
			assert.Equal(t, map[string][]string{"paris-1": {}}, typesOf(t, repo, "paris"), "a probe that listed nothing ran every type")
			assert.Empty(t, typesOf(t, repo, "lyon"))
			kept, err := repo.FindByID(ctx, "frankfurt")
			require.NoError(t, err)
			assert.Equal(t, []string{"http", "tcp"}, kept.SupportedTypes, "the probe's own list is left as it was")
			require.NoError(t, repo.UpsertInstance(ctx, "lyon", &probe.Instance{ID: uuid.NewString(), LastSeenAt: seen}), "an instance added after the migration is fine")
		})
	}
}

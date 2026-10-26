package probe_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"
	"peekaping/internal/modules/probe"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// sqliteDB opens a SQLite database the way the app does, with foreign keys on.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "probe.db")}, zap.NewNop().Sugar())
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
	schema := "probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

// migrateDB migrates the database as the startup script does with cmd/bun
func migrateDB(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
}

// forEachSQLBackend runs fn on a migrated database of every SQL storage backend. PostgreSQL runs
// when PEE_TEST_POSTGRES_DSN is set.
func forEachSQLBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) {
			db := open(t)
			migrateDB(t, db)
			fn(t, db)
		})
	}
}

func TestSQLRepository_LocalProbeIsSeededByTheMigration(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)

		local, err := repo.FindByID(context.Background(), "local")

		require.NoError(t, err)
		require.NotNil(t, local)
		assert.Equal(t, "local", local.ID)
		assert.Equal(t, "Local", local.Name)
		assert.Equal(t, "", local.Location)
		assert.Equal(t, "", local.TokenHash, "no token until a bootstrap sets one")
		assert.Equal(t, "", local.TokenPrefix)
		assert.False(t, local.Disabled)
		assert.Equal(t, probe.StatusOffline, local.Status)
		assert.Equal(t, []string{}, local.SupportedTypes)
		assert.False(t, local.CreatedAt.IsZero())
	})
}

func TestSQLRepository_CreatesFindsUpdatesAndDeletes(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)

		created, err := repo.Create(ctx, &probe.CreateModel{Name: "Frankfurt", Location: "eu-central", TokenHash: "sha256$aa", TokenPrefix: "pkp_aaaa"})
		require.NoError(t, err)
		_, parseErr := uuid.Parse(created.ID)
		assert.NoError(t, parseErr, "a probe's id is a UUID")
		assert.Equal(t, "Frankfurt", created.Name)
		assert.Equal(t, "eu-central", created.Location)
		assert.Equal(t, "pkp_aaaa", created.TokenPrefix)
		assert.Equal(t, probe.StatusOffline, created.Status)
		assert.False(t, created.Disabled)

		found, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, created.ID, found.ID)
		assert.Equal(t, "sha256$aa", found.TokenHash)
		assert.WithinDuration(t, time.Now(), found.CreatedAt, time.Minute)

		byHash, err := repo.FindByTokenHash(ctx, "sha256$aa")
		require.NoError(t, err)
		require.NotNil(t, byHash)
		assert.Equal(t, created.ID, byHash.ID)
		missing, err := repo.FindByTokenHash(ctx, "sha256$bb")
		require.NoError(t, err)
		assert.Nil(t, missing)

		all, err := repo.FindAll(ctx)
		require.NoError(t, err)
		ids := []string{}
		for _, p := range all {
			ids = append(ids, p.ID)
		}
		assert.ElementsMatch(t, []string{"local", created.ID}, ids)

		name, location, disabled := "Paris", "eu-west", true
		updated, err := repo.Update(ctx, created.ID, &probe.UpdateModel{Name: &name, Location: &location, Disabled: &disabled})
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, "Paris", updated.Name)
		assert.Equal(t, "eu-west", updated.Location)
		assert.True(t, updated.Disabled)
		assert.Equal(t, "sha256$aa", updated.TokenHash, "an update that names no token keeps it")

		onlyName := "Lyon"
		partial, err := repo.Update(ctx, created.ID, &probe.UpdateModel{Name: &onlyName})
		require.NoError(t, err)
		assert.Equal(t, "Lyon", partial.Name)
		assert.Equal(t, "eu-west", partial.Location)
		assert.True(t, partial.Disabled)

		absent, err := repo.Update(ctx, uuid.NewString(), &probe.UpdateModel{Name: &onlyName})
		require.NoError(t, err)
		assert.Nil(t, absent)

		require.NoError(t, repo.UpdateToken(ctx, created.ID, "sha256$cc", "pkp_cccc"))
		oldToken, err := repo.FindByTokenHash(ctx, "sha256$aa")
		require.NoError(t, err)
		assert.Nil(t, oldToken)
		newToken, err := repo.FindByTokenHash(ctx, "sha256$cc")
		require.NoError(t, err)
		require.NotNil(t, newToken)
		assert.Equal(t, "pkp_cccc", newToken.TokenPrefix)

		require.NoError(t, repo.Delete(ctx, created.ID))
		gone, err := repo.FindByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Nil(t, gone)
	})
}

func TestSQLRepository_CreateKeepsAGivenID(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		repo := probe.NewSQLRepository(db)

		created, err := repo.Create(context.Background(), &probe.CreateModel{ID: "edge", Name: "Edge", TokenHash: "sha256$dd", TokenPrefix: "tokenabc"})

		require.NoError(t, err)
		assert.Equal(t, "edge", created.ID)
	})
}

func TestSQLRepository_FindInstancesListsTheProbesInstances(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		other, err := repo.Create(ctx, &probe.CreateModel{Name: "Other", TokenHash: "sha256$ee", TokenPrefix: "pkp_eeee"})
		require.NoError(t, err)

		none, err := repo.FindInstances(ctx, "local")
		require.NoError(t, err)
		assert.Empty(t, none)

		instanceID := uuid.NewString()
		lastSeen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
		_, err = db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES (?, 'local', 'host-1', 'linux', 'arm64', '1.2.3', 1, ?)`, instanceID, lastSeen)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES (?, ?, 'host-2', 'linux', 'amd64', '1.2.3', 1, ?)`, uuid.NewString(), other.ID, lastSeen)
		require.NoError(t, err)

		instances, err := repo.FindInstances(ctx, "local")

		require.NoError(t, err)
		require.Len(t, instances, 1)
		assert.Equal(t, instanceID, instances[0].ID)
		assert.Equal(t, "host-1", instances[0].Hostname)
		assert.Equal(t, "linux", instances[0].OS)
		assert.Equal(t, "arm64", instances[0].Arch)
		assert.Equal(t, "1.2.3", instances[0].Version)
		assert.Equal(t, 1, instances[0].ProtocolVersion)
		assert.WithinDuration(t, lastSeen, instances[0].LastSeenAt, time.Second)
	})
}

func TestSQLRepository_DeleteRemovesTheProbesInstances(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		created, err := repo.Create(ctx, &probe.CreateModel{Name: "Short-lived", TokenHash: "sha256$ff", TokenPrefix: "pkp_ffff"})
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO probe_instances (id, probe_id, hostname, os, arch, version, protocol_version, last_seen_at) VALUES (?, ?, 'host', 'linux', 'amd64', '1.0.0', 1, ?)`, uuid.NewString(), created.ID, time.Now())
		require.NoError(t, err)

		require.NoError(t, repo.Delete(ctx, created.ID))

		var left int
		require.NoError(t, db.NewSelect().Table("probe_instances").ColumnExpr("count(*)").Where("probe_id = ?", created.ID).Scan(ctx, &left))
		assert.Zero(t, left)
	})
}

// addMonitorOn stores a monitor that runs on the probe
func addMonitorOn(t *testing.T, db *bun.DB, probeID string, active bool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec(`INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval, active) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0, ?)`, id, active)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO monitor_probes (monitor_id, probe_id) VALUES (?, ?)", id, probeID)
	require.NoError(t, err)
	return id
}

func TestSQLRepository_CountMonitorsCountsTheMonitorsOfTheProbe(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		first, second := twoProbes(t, repo)
		addMonitorOn(t, db, first.ID, true)
		addMonitorOn(t, db, first.ID, false)
		addMonitorOn(t, db, second.ID, true)

		onFirst, err := repo.CountMonitors(ctx, first.ID)
		require.NoError(t, err)
		onLocal, err := repo.CountMonitors(ctx, probe.LocalID)
		require.NoError(t, err)
		onUnknown, err := repo.CountMonitors(ctx, uuid.NewString())
		require.NoError(t, err)

		assert.Equal(t, 2, onFirst, "a paused monitor runs on its probe, too")
		assert.Zero(t, onLocal)
		assert.Zero(t, onUnknown)
	})
}

func TestSQLRepository_DeleteIsRefusedForAProbeWithMonitors(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		first, _ := twoProbes(t, repo)
		addMonitorOn(t, db, first.ID, true)

		assert.ErrorIs(t, repo.Delete(ctx, first.ID), probe.ErrProbeHasMonitors, "the database holds the line, even for a caller that didn't count")

		found, err := repo.FindByID(ctx, first.ID)
		require.NoError(t, err)
		assert.NotNil(t, found)
	})
}

// twoProbes makes two probes to register instances on
func twoProbes(t *testing.T, repo probe.Repository) (first, second *probe.Model) {
	t.Helper()
	first, err := repo.Create(context.Background(), &probe.CreateModel{Name: "First", TokenHash: "sha256$a1", TokenPrefix: "pkp_a1"})
	require.NoError(t, err)
	second, err = repo.Create(context.Background(), &probe.CreateModel{Name: "Second", TokenHash: "sha256$b2", TokenPrefix: "pkp_b2"})
	require.NoError(t, err)
	return first, second
}

func TestSQLRepository_UpsertInstanceInsertsThenUpdatesEveryColumn(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		instanceID := uuid.NewString()
		first := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)

		require.NoError(t, repo.UpsertInstance(ctx, "local", &probe.Instance{
			ID: instanceID, Hostname: "host-1", OS: "linux", Arch: "arm64", Version: "1.2.3", ProtocolVersion: 1, LastSeenAt: first,
		}))
		instances, err := repo.FindInstances(ctx, "local")
		require.NoError(t, err)
		require.Len(t, instances, 1)
		assert.Equal(t, instanceID, instances[0].ID)
		assert.Equal(t, "host-1", instances[0].Hostname)
		assert.Equal(t, "linux", instances[0].OS)
		assert.Equal(t, "arm64", instances[0].Arch)
		assert.Equal(t, "1.2.3", instances[0].Version)
		assert.Equal(t, 1, instances[0].ProtocolVersion)
		assert.WithinDuration(t, first, instances[0].LastSeenAt, time.Second)

		later := first.Add(time.Hour)
		require.NoError(t, repo.UpsertInstance(ctx, "local", &probe.Instance{
			ID: instanceID, Hostname: "host-2", OS: "darwin", Arch: "amd64", Version: "1.3.0", ProtocolVersion: 2, LastSeenAt: later,
		}))
		instances, err = repo.FindInstances(ctx, "local")
		require.NoError(t, err)
		require.Len(t, instances, 1, "the same instance id is the same instance")
		assert.Equal(t, "host-2", instances[0].Hostname)
		assert.Equal(t, "darwin", instances[0].OS)
		assert.Equal(t, "amd64", instances[0].Arch)
		assert.Equal(t, "1.3.0", instances[0].Version)
		assert.Equal(t, 2, instances[0].ProtocolVersion)
		assert.WithinDuration(t, later, instances[0].LastSeenAt, time.Second)
	})
}

// An instance id is the primary key, so a probe's hello must not take over another probe's instance
func TestSQLRepository_UpsertInstanceRefusesAnotherProbesInstance(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, intruder := twoProbes(t, repo)
		seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "shared-id", Hostname: "owner-host", LastSeenAt: seen}))

		err := repo.UpsertInstance(ctx, intruder.ID, &probe.Instance{ID: "shared-id", Hostname: "intruder-host", LastSeenAt: seen.Add(time.Hour)})

		assert.ErrorIs(t, err, probe.ErrInstanceTaken)
		ownersInstances, err := repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		require.Len(t, ownersInstances, 1)
		assert.Equal(t, "owner-host", ownersInstances[0].Hostname)
		assert.WithinDuration(t, seen, ownersInstances[0].LastSeenAt, time.Second)
		intrudersInstances, err := repo.FindInstances(ctx, intruder.ID)
		require.NoError(t, err)
		assert.Empty(t, intrudersInstances)
	})
}

// instanceIDs are the ids of the instances the probe holds, sorted
func instanceIDs(t *testing.T, repo probe.Repository, probeID string) []string {
	t.Helper()
	instances, err := repo.FindInstances(context.Background(), probeID)
	require.NoError(t, err)
	ids := []string{}
	for _, instance := range instances {
		ids = append(ids, instance.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestSQLRepository_MakeRoomForInstanceDeletesTheOldestInstancesThatAreQuiet(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		quietBefore := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for probeID, seen := range map[string]map[string]time.Time{
			owner.ID: {
				"owner-oldest":   quietBefore.Add(-3 * time.Hour),
				"owner-older":    quietBefore.Add(-2 * time.Hour),
				"owner-at-edge":  quietBefore, // seen exactly at the edge counts as online
				"owner-online-1": quietBefore.Add(time.Minute),
			},
			other.ID: {"other-oldest": quietBefore.Add(-48 * time.Hour)},
		} {
			for id, at := range seen {
				require.NoError(t, repo.UpsertInstance(ctx, probeID, &probe.Instance{ID: id, LastSeenAt: at}))
			}
		}

		t.Run("below the limit it deletes nothing", func(t *testing.T) {
			require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 5, quietBefore))

			assert.Len(t, instanceIDs(t, repo, owner.ID), 4)
		})

		t.Run("at the limit it deletes the oldest quiet one, and no other probe's", func(t *testing.T) {
			require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 4, quietBefore))

			assert.Equal(t, []string{"owner-at-edge", "owner-older", "owner-online-1"}, instanceIDs(t, repo, owner.ID))
			assert.Equal(t, []string{"other-oldest"}, instanceIDs(t, repo, other.ID), "another probe's quiet instance is out of reach")
		})

		t.Run("over the limit it deletes as many as it takes to leave one place", func(t *testing.T) {
			for id, at := range map[string]time.Time{"owner-extra-1": quietBefore.Add(-5 * time.Hour), "owner-extra-2": quietBefore.Add(-4 * time.Hour)} {
				require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: id, LastSeenAt: at}))
			}
			require.Len(t, instanceIDs(t, repo, owner.ID), 5)

			require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 3, quietBefore))

			assert.Equal(t, []string{"owner-at-edge", "owner-online-1"}, instanceIDs(t, repo, owner.ID), "from 5 to 2: the limit less the place")
		})
	})
}

func TestSQLRepository_MakeRoomForInstanceRefusesWhenTooFewAreQuiet(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		quietBefore := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for id, at := range map[string]time.Time{
			"quiet":    quietBefore.Add(-time.Hour),
			"online-1": quietBefore,
			"online-2": quietBefore.Add(time.Minute),
		} {
			require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: id, LastSeenAt: at}))
		}

		assert.ErrorIs(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 2, quietBefore), probe.ErrTooManyInstances,
			"3 held and a limit of 2 need 2 places, and only 1 is quiet")
		assert.Equal(t, []string{"online-1", "online-2", "quiet"}, instanceIDs(t, repo, owner.ID), "a refusal deletes nothing")

		require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 3, quietBefore))
		assert.Equal(t, []string{"online-1", "online-2"}, instanceIDs(t, repo, owner.ID))
		assert.ErrorIs(t, repo.MakeRoomForInstance(ctx, owner.ID, "owner-new", 2, quietBefore), probe.ErrTooManyInstances,
			"every instance is online now")
	})
}

func TestSQLRepository_MakeRoomForInstanceLeavesAnInstanceTheProbeHoldsAlone(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		quietBefore := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for id, at := range map[string]time.Time{"quiet": quietBefore.Add(-time.Hour), "online": quietBefore.Add(time.Minute)} {
			require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: id, LastSeenAt: at}))
		}

		require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "online", 2, quietBefore), "its hello only updates it")
		require.NoError(t, repo.MakeRoomForInstance(ctx, owner.ID, "quiet", 2, quietBefore))
		assert.Equal(t, []string{"online", "quiet"}, instanceIDs(t, repo, owner.ID), "so nobody's place is taken")

		err := repo.MakeRoomForInstance(ctx, other.ID, "online", 1, quietBefore)
		assert.NoError(t, err, "another probe holds no instance, so it has room")
	})
}

func TestSQLRepository_TouchInstanceStampsOnlyTheProbesOwnInstance(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		seen := time.Date(2026, 10, 22, 8, 30, 0, 0, time.UTC)
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "instance-1", Hostname: "host", LastSeenAt: seen}))

		found, err := repo.TouchInstance(ctx, owner.ID, "instance-1", seen.Add(time.Minute))
		require.NoError(t, err)
		assert.True(t, found)
		instances, err := repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		require.Len(t, instances, 1)
		assert.WithinDuration(t, seen.Add(time.Minute), instances[0].LastSeenAt, time.Second)
		assert.Equal(t, "host", instances[0].Hostname, "a touch changes only the last seen time")

		found, err = repo.TouchInstance(ctx, owner.ID, "never-said-hello", seen.Add(time.Hour))
		require.NoError(t, err)
		assert.False(t, found, "no row, no stamp")

		found, err = repo.TouchInstance(ctx, other.ID, "instance-1", seen.Add(time.Hour))
		require.NoError(t, err)
		assert.False(t, found, "another probe's token doesn't reach the instance")
		instances, err = repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		assert.WithinDuration(t, seen.Add(time.Minute), instances[0].LastSeenAt, time.Second)
	})
}

func TestSQLRepository_RetireInstanceSetsOnlyTheProbesOwnInstancesLastSeen(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		now := time.Now().UTC()
		retired := now.Add(-time.Minute)
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "instance-1", Hostname: "host-1", LastSeenAt: now}))
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "instance-2", LastSeenAt: now}))

		require.NoError(t, repo.RetireInstance(ctx, other.ID, "instance-1", retired, retired), "another probe's token retires nothing")
		instances, err := repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		require.Len(t, instances, 2)
		for _, instance := range instances {
			assert.WithinDuration(t, now, instance.LastSeenAt, time.Second)
		}

		require.NoError(t, repo.RetireInstance(ctx, owner.ID, "instance-1", retired, retired))
		instances, err = repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		require.Len(t, instances, 2, "the row stays")
		assert.Equal(t, "instance-2", instances[0].ID, "the most recently seen is first")
		assert.WithinDuration(t, now, instances[0].LastSeenAt, time.Second)
		assert.Equal(t, "instance-1", instances[1].ID)
		assert.WithinDuration(t, retired, instances[1].LastSeenAt, time.Second)
		assert.Equal(t, "host-1", instances[1].Hostname, "and keeps what else it says")

		require.NoError(t, repo.RetireInstance(ctx, owner.ID, "never-there", retired, retired), "retiring an instance that is gone is not an error")
		instances, err = repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		assert.Len(t, instances, 2)
	})
}

func TestSQLRepository_SetSupportedTypesReplacesTheList(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)

		require.NoError(t, repo.SetSupportedTypes(ctx, owner.ID, []string{"http", "tcp"}))
		got, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"http", "tcp"}, got.SupportedTypes)

		require.NoError(t, repo.SetSupportedTypes(ctx, owner.ID, []string{"ping"}))
		got, err = repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{"ping"}, got.SupportedTypes, "the new list replaces the old one")

		untouched, err := repo.FindByID(ctx, other.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{}, untouched.SupportedTypes)

		require.NoError(t, repo.SetSupportedTypes(ctx, owner.ID, nil))
		got, err = repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, []string{}, got.SupportedTypes, "nil is an empty list, not null")
	})
}

func TestSQLRepository_UpdateStatusChangesOnlyAProbeInTheStatusItIsGiven(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		before, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)

		changed, err := repo.UpdateStatus(ctx, owner.ID, probe.StatusOnline, probe.StatusOffline, time.Now())
		require.NoError(t, err)
		assert.False(t, changed, "the probe is offline, not online: nothing to change")

		changed, err = repo.UpdateStatus(ctx, owner.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		assert.True(t, changed)
		got, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOnline, got.Status)
		assert.True(t, got.UpdatedAt.After(before.UpdatedAt), "a status change touches updated_at")

		changed, err = repo.UpdateStatus(ctx, owner.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		assert.False(t, changed, "the second of two racing callers changes nothing")

		untouched, err := repo.FindByID(ctx, other.ID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOffline, untouched.Status)

		changed, err = repo.UpdateStatus(ctx, "no-such-probe", probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		assert.False(t, changed)
	})
}

// A request authenticates while the probe is enabled, an admin disables it, and then the request
// brings the probe online: the update has to see the probe as it is now
func TestSQLRepository_UpdateStatusLeavesADisabledProbeOffline(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		disabled := true
		_, err := repo.Update(ctx, owner.ID, &probe.UpdateModel{Disabled: &disabled})
		require.NoError(t, err)

		changed, err := repo.UpdateStatus(ctx, owner.ID, probe.StatusOffline, probe.StatusOnline, time.Now())

		require.NoError(t, err)
		assert.False(t, changed, "a disabled probe is not brought online")
		got, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOffline, got.Status)

		changed, err = repo.UpdateStatus(ctx, other.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		assert.True(t, changed, "an enabled probe still comes online")
	})
}

// The sweep sets a disabled probe offline once it goes quiet, which it must still be able to do
func TestSQLRepository_UpdateStatusStillSetsADisabledProbeOffline(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, _ := twoProbes(t, repo)
		changed, err := repo.UpdateStatus(ctx, owner.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
		require.NoError(t, err)
		require.True(t, changed)
		disabled := true
		_, err = repo.Update(ctx, owner.ID, &probe.UpdateModel{Disabled: &disabled})
		require.NoError(t, err)

		changed, err = repo.UpdateStatus(ctx, owner.ID, probe.StatusOnline, probe.StatusOffline, time.Now())

		require.NoError(t, err)
		assert.True(t, changed)
		got, err := repo.FindByID(ctx, owner.ID)
		require.NoError(t, err)
		assert.Equal(t, probe.StatusOffline, got.Status)
		assert.True(t, got.Disabled, "the status change leaves the probe disabled")
	})
}

func TestSQLRepository_DeleteInstancesUnseenSinceRemovesTheOldOnes(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		cutoff := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
		for probeID, seen := range map[string]map[string]time.Time{
			owner.ID: {"owner-old": cutoff.Add(-time.Hour), "owner-new": cutoff.Add(time.Hour), "owner-at-cutoff": cutoff},
			other.ID: {"other-old": cutoff.Add(-48 * time.Hour)},
		} {
			for id, at := range seen {
				require.NoError(t, repo.UpsertInstance(ctx, probeID, &probe.Instance{ID: id, LastSeenAt: at}))
			}
		}

		removed, err := repo.DeleteInstancesUnseenSince(ctx, cutoff)

		require.NoError(t, err)
		assert.Equal(t, 2, removed)
		left, err := repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		ids := []string{}
		for _, instance := range left {
			ids = append(ids, instance.ID)
		}
		assert.ElementsMatch(t, []string{"owner-new", "owner-at-cutoff"}, ids, "only what was last seen before the cutoff goes")
		gone, err := repo.FindInstances(ctx, other.ID)
		require.NoError(t, err)
		assert.Empty(t, gone)

		removed, err = repo.DeleteInstancesUnseenSince(ctx, cutoff)
		require.NoError(t, err)
		assert.Zero(t, removed)
	})
}

// Every repository call made with the ctx WithTx gives runs on that transaction. The database has
// one connection here, as SQLite does in the app, so a call that ran on db instead would wait for
// the connection the transaction holds, and fail when ctx times out.
func TestSQLRepository_EveryMethodJoinsTheCallersTransaction(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		db.SetMaxOpenConns(1)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		repo := probe.NewSQLRepository(db)
		keep, err := repo.Create(ctx, &probe.CreateModel{Name: "Keep", TokenHash: "sha256$k1", TokenPrefix: "pkp_kkk1"})
		require.NoError(t, err)
		errRollBack := errors.New("roll back")

		err = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			created, err := repo.Create(ctx, &probe.CreateModel{Name: "Doomed", TokenHash: "sha256$d1", TokenPrefix: "pkp_ddd1"})
			require.NoError(t, err)
			name := "Renamed"
			renamed, err := repo.Update(ctx, keep.ID, &probe.UpdateModel{Name: &name})
			require.NoError(t, err)
			assert.Equal(t, "Renamed", renamed.Name, "the transaction reads its own writes")
			require.NoError(t, repo.UpdateToken(ctx, keep.ID, "sha256$k2", "pkp_kkk2"))
			byHash, err := repo.FindByTokenHash(ctx, "sha256$k2")
			require.NoError(t, err)
			require.NotNil(t, byHash)
			all, err := repo.FindAll(ctx)
			require.NoError(t, err)
			assert.Len(t, all, 3)
			_, err = repo.FindInstances(ctx, keep.ID)
			require.NoError(t, err)
			seen := time.Now().UTC()
			require.NoError(t, repo.UpsertInstance(ctx, keep.ID, &probe.Instance{ID: "instance-1", Hostname: "host-1", LastSeenAt: seen}))
			require.NoError(t, repo.UpsertInstance(ctx, keep.ID, &probe.Instance{ID: "instance-2", Hostname: "host-2", LastSeenAt: seen}))
			require.ErrorIs(t, repo.MakeRoomForInstance(ctx, keep.ID, "instance-3", 2, seen.Add(-24*time.Hour)), probe.ErrTooManyInstances)
			touched, err := repo.TouchInstance(ctx, keep.ID, "instance-1", seen.Add(time.Minute))
			require.NoError(t, err)
			assert.True(t, touched)
			require.NoError(t, repo.RetireInstance(ctx, keep.ID, "instance-2", seen.Add(-time.Hour), seen.Add(-time.Hour)))
			require.NoError(t, repo.SetSupportedTypes(ctx, keep.ID, []string{"http"}))
			flipped, err := repo.UpdateStatus(ctx, keep.ID, probe.StatusOffline, probe.StatusOnline, time.Now())
			require.NoError(t, err)
			assert.True(t, flipped)
			_, err = repo.DeleteInstancesUnseenSince(ctx, seen.Add(-24*time.Hour))
			require.NoError(t, err)
			instances, err := repo.FindInstances(ctx, keep.ID)
			require.NoError(t, err)
			require.Len(t, instances, 2)
			assert.WithinDuration(t, seen.Add(-time.Hour), instances[1].LastSeenAt, time.Second, "the transaction reads its own instance writes")
			require.NoError(t, repo.Delete(ctx, created.ID))
			return errRollBack
		})

		require.ErrorIs(t, err, errRollBack)
		kept, err := repo.FindByID(ctx, keep.ID)
		require.NoError(t, err)
		require.NotNil(t, kept)
		assert.Equal(t, "Keep", kept.Name, "the rename was rolled back")
		assert.Equal(t, "sha256$k1", kept.TokenHash, "and so was the new token")
		assert.Empty(t, kept.SupportedTypes, "and the supported types")
		assert.Equal(t, probe.StatusOffline, kept.Status, "and the status flip")
		instances, err := repo.FindInstances(ctx, keep.ID)
		require.NoError(t, err)
		assert.Empty(t, instances, "and the instance upserts")
		all, err := repo.FindAll(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 2, "local and Keep: Doomed was rolled back")
	})
}

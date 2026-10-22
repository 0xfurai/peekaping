package probe_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
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

func TestSQLRepository_DeleteInstanceRemovesOnlyTheProbesOwnInstance(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		repo := probe.NewSQLRepository(db)
		owner, other := twoProbes(t, repo)
		now := time.Now().UTC()
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "instance-1", LastSeenAt: now}))
		require.NoError(t, repo.UpsertInstance(ctx, owner.ID, &probe.Instance{ID: "instance-2", LastSeenAt: now}))

		require.NoError(t, repo.DeleteInstance(ctx, other.ID, "instance-1"), "another probe's token removes nothing")
		instances, err := repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		assert.Len(t, instances, 2)

		require.NoError(t, repo.DeleteInstance(ctx, owner.ID, "instance-1"))
		instances, err = repo.FindInstances(ctx, owner.ID)
		require.NoError(t, err)
		require.Len(t, instances, 1)
		assert.Equal(t, "instance-2", instances[0].ID)

		require.NoError(t, repo.DeleteInstance(ctx, owner.ID, "instance-1"), "removing an instance that is gone is not an error")
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
			touched, err := repo.TouchInstance(ctx, keep.ID, "instance-1", seen.Add(time.Minute))
			require.NoError(t, err)
			assert.True(t, touched)
			require.NoError(t, repo.DeleteInstance(ctx, keep.ID, "instance-2"))
			require.NoError(t, repo.SetSupportedTypes(ctx, keep.ID, []string{"http"}))
			instances, err := repo.FindInstances(ctx, keep.ID)
			require.NoError(t, err)
			require.Len(t, instances, 1, "the transaction reads its own instance writes")
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
		instances, err := repo.FindInstances(ctx, keep.ID)
		require.NoError(t, err)
		assert.Empty(t, instances, "and the instance upserts")
		all, err := repo.FindAll(ctx)
		require.NoError(t, err)
		assert.Len(t, all, 2, "local and Keep: Doomed was rolled back")
	})
}

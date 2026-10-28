package workspace_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/config"
	"peekaping/internal/infra"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

const (
	// workspacesMigration creates the tables and adoptMigration gives an install its workspace (PP-146)
	workspacesMigration = "20261101120000"
	adoptMigration      = "20261101120100"
)

// sqliteDB opens a SQLite database the way the app does, with one connection.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "workspaces.db")}, zap.NewNop().Sugar())
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
	schema := "workspaces_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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

func forEachUnmigratedBackend(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	for name, open := range map[string]func(*testing.T) *bun.DB{"SQLite": sqliteDB, "Postgres": postgresDB} {
		t.Run(name, func(t *testing.T) { fn(t, open(t)) })
	}
}

// migratedBefore migrates the database as it was before the migration named first, and returns
// the migrator that has every migration, to upgrade it with. The migrator marks a migration
// applied only when it succeeded, as the app's does (cmd/bun/migrate.go).
func migratedBefore(t *testing.T, db *bun.DB, first string) *migrate.Migrator {
	t.Helper()
	ctx := context.Background()
	before := migrate.NewMigrations()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name < first {
			before.Add(m)
		}
	}
	old := migrate.NewMigrator(db, before)
	require.NoError(t, old.Init(ctx))
	_, err := old.Migrate(ctx)
	require.NoError(t, err)
	return migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
}

func insertUser(t *testing.T, db *bun.DB, email string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := db.Exec("INSERT INTO users (id, email, password) VALUES (?, ?, 'hash')", id, email)
	require.NoError(t, err)
	return id
}

// records are one row for each table a workspace will own, apart from the users
var records = map[string]string{
	"monitors":              `INSERT INTO monitors (id, type, name, interval, timeout, max_retries, retry_interval, resend_interval) VALUES (?, 'http', 'Monitor', 60, 10, 1, 10, 0)`,
	"notification_channels": `INSERT INTO notification_channels (id, name, type) VALUES (?, 'Channel', 'webhook')`,
	"proxies":               `INSERT INTO proxies (id, protocol, host, port) VALUES (?, 'http', 'proxy.example.com', 8080)`,
	"tags":                  `INSERT INTO tags (id, name) VALUES (?, 'Tag')`,
	"maintenances":          `INSERT INTO maintenances (id, title, strategy) VALUES (?, 'Window', 'manual')`,
	"status_pages":          `INSERT INTO status_pages (id, slug, title) VALUES (?, 'status', 'Status')`,
	"api_keys":              `INSERT INTO api_keys (id, name) VALUES (?, 'Key')`,
}

type workspaceRow struct {
	ID   string `bun:"id"`
	Name string `bun:"name"`
}

type memberRow struct {
	WorkspaceID string `bun:"workspace_id"`
	UserID      string `bun:"user_id"`
	Role        string `bun:"role"`
}

func workspaces(t *testing.T, db *bun.DB) []workspaceRow {
	t.Helper()
	rows := []workspaceRow{}
	require.NoError(t, db.NewSelect().Table("workspaces").Column("id", "name").Order("id").Scan(context.Background(), &rows))
	return rows
}

func members(t *testing.T, db *bun.DB) []memberRow {
	t.Helper()
	rows := []memberRow{}
	require.NoError(t, db.NewSelect().Table("workspace_members").Column("workspace_id", "user_id", "role").Order("user_id").Scan(context.Background(), &rows))
	return rows
}

func instanceAdmins(t *testing.T, db *bun.DB) map[string]bool {
	t.Helper()
	var rows []struct {
		ID            string `bun:"id"`
		InstanceAdmin bool   `bun:"instance_admin"`
	}
	require.NoError(t, db.NewSelect().Table("users").Column("id", "instance_admin").Scan(context.Background(), &rows))
	admins := map[string]bool{}
	for _, r := range rows {
		admins[r.ID] = r.InstanceAdmin
	}
	return admins
}

// adoptAgain runs the Go migration a second time, as a re-run of the upgrade would
func adoptAgain(t *testing.T, db *bun.DB) {
	t.Helper()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name == adoptMigration {
			require.NoError(t, m.Up(context.Background(), db, nil))
			return
		}
	}
	t.Fatalf("migration %s not found", adoptMigration)
}

func TestMigrations_AnUpgradedInstallHasOneWorkspaceWithEveryUserItsOwnerAndAnInstanceAdmin(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		first, second := insertUser(t, db, "first@example.com"), insertUser(t, db, "second@example.com")
		for _, insert := range records {
			_, err := db.Exec(insert, uuid.NewString())
			require.NoError(t, err)
		}

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		ws := workspaces(t, db)
		require.Len(t, ws, 1)
		assert.Equal(t, "Default", ws[0].Name)
		assert.ElementsMatch(t, []memberRow{{ws[0].ID, first, "owner"}, {ws[0].ID, second, "owner"}}, members(t, db))
		assert.Equal(t, map[string]bool{first: true, second: true}, instanceAdmins(t, db))
	})
}

func TestMigrations_RunningTheAdoptionAgainChangesNothing(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		first := insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		want := workspaces(t, db)

		// A user who signed up since, and an admin who gave up the flag, are what a re-run must leave alone
		later := insertUser(t, db, "later@example.com")
		_, err = db.Exec("UPDATE users SET instance_admin = false WHERE id = ?", first)
		require.NoError(t, err)
		adoptAgain(t, db)

		assert.Equal(t, want, workspaces(t, db))
		assert.Len(t, members(t, db), 1, "no membership for the later user")
		assert.Equal(t, map[string]bool{first: false, later: false}, instanceAdmins(t, db))
	})
}

func TestMigrations_AnEmptyInstallGetsNoWorkspace(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		upgrade := migratedBefore(t, db, workspacesMigration)

		_, err := upgrade.Migrate(context.Background())
		require.NoError(t, err)
		adoptAgain(t, db)

		assert.Empty(t, workspaces(t, db), "the first signup makes the workspace")
		assert.Empty(t, members(t, db))
	})
}

func TestMigrations_AnInstallWithRecordsAndNoUserGetsTheWorkspaceForTheFirstSignup(t *testing.T) {
	for table, insert := range records {
		t.Run(table, func(t *testing.T) {
			forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
				upgrade := migratedBefore(t, db, workspacesMigration)
				_, err := db.Exec(insert, uuid.NewString())
				require.NoError(t, err)

				_, err = upgrade.Migrate(context.Background())
				require.NoError(t, err)

				ws := workspaces(t, db)
				require.Len(t, ws, 1)
				assert.Equal(t, "Default", ws[0].Name)
				assert.Empty(t, members(t, db))
			})
		})
	}
}

func TestMigrations_AnExistingWorkspaceIsLeftAlone(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, adoptMigration)
		// Stop after the tables: the install already made its workspace, as one that ran the adoption would
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("DELETE FROM bun_migrations WHERE name = ?", adoptMigration)
		require.NoError(t, err)
		owner := insertUser(t, db, "owner@example.com")
		other := insertUser(t, db, "other@example.com")
		workspaceID := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Mine')", workspaceID)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'owner')", workspaceID, owner)
		require.NoError(t, err)

		adoptAgain(t, db)

		assert.Equal(t, []workspaceRow{{workspaceID, "Mine"}}, workspaces(t, db))
		assert.Equal(t, []memberRow{{workspaceID, owner, "owner"}}, members(t, db))
		assert.Equal(t, map[string]bool{owner: false, other: false}, instanceAdmins(t, db))
	})
}

func TestMigrations_TheWorkspaceTablesGoDownAndComeBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		user := insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT instance_admin FROM users")
		assert.Error(t, err, "the flag goes")
		_, err = db.Exec("SELECT 1 FROM workspaces")
		assert.Error(t, err, "the tables go")
		var email string
		require.NoError(t, db.NewSelect().Table("users").Column("email").Where("id = ?", user).Scan(ctx, &email), "the user stays")

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		assert.Len(t, workspaces(t, db), 1, "the user's install is adopted again")
		assert.Equal(t, map[string]bool{user: true}, instanceAdmins(t, db))
	})
}

// Both databases keep the members of a deleted workspace and the places of a deleted user from
// outliving them
func TestMigrations_MembershipsGoWithTheirWorkspaceAndTheirUser(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		if db.Dialect().Name() == dialect.SQLite {
			_, err := db.Exec("PRAGMA foreign_keys = ON")
			require.NoError(t, err)
		}
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		keep, drop := insertUser(t, db, "keep@example.com"), insertUser(t, db, "drop@example.com")
		a, b := uuid.NewString(), uuid.NewString()
		for _, id := range []string{a, b} {
			_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'W')", id)
			require.NoError(t, err)
		}
		for _, m := range [][2]string{{a, keep}, {a, drop}, {b, keep}} {
			_, err = db.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')", m[0], m[1])
			require.NoError(t, err)
		}

		_, err = db.Exec("DELETE FROM users WHERE id = ?", drop)
		require.NoError(t, err)
		assert.Len(t, members(t, db), 2, "the deleted user's place goes")
		_, err = db.Exec("DELETE FROM workspaces WHERE id = ?", b)
		require.NoError(t, err)
		assert.Equal(t, []memberRow{{a, keep, "member"}}, members(t, db), "the deleted workspace's members go")
	})
}

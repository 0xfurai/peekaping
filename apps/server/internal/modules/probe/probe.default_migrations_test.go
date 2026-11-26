package probe_test

import (
	"context"
	"testing"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/modules/probe"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// addAdminAreaMigration is the migration that adds the default probes and the last sign-in (PP-371)
const addAdminAreaMigration = "20261110120000"

// migratedBeforeAdminArea migrates the database as it was before the admin area, and returns the
// migrator that has every migration, to upgrade it with
func migratedBeforeAdminArea(t *testing.T, db *bun.DB) *migrate.Migrator {
	t.Helper()
	ctx := context.Background()
	before := migrate.NewMigrations()
	for _, m := range migrations.Migrations.Sorted() {
		if m.Name < addAdminAreaMigration {
			before.Add(m)
		}
	}
	old := migrate.NewMigrator(db, before)
	require.NoError(t, old.Init(ctx))
	_, err := old.Migrate(ctx)
	require.NoError(t, err)
	return migrate.NewMigrator(db, migrations.Migrations)
}

func TestMigrations_AnUpgradedInstallRunsNewMonitorsOnTheLocalProbeAsBefore(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBeforeAdminArea(t, db)
		remote := uuid.NewString()
		_, err := db.Exec("INSERT INTO probes (id, name, token_hash) VALUES (?, ?, ?)", remote, "Frankfurt", "sha256$frankfurt")
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO users (id, email, password) VALUES (?, 'user@example.com', 'hash')", uuid.NewString())
		require.NoError(t, err)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		repo := probe.NewSQLRepository(db)
		local, err := repo.FindByID(ctx, probe.LocalID)
		require.NoError(t, err)
		assert.True(t, local.IsDefault, "the local probe is the default one")
		other, err := repo.FindByID(ctx, remote)
		require.NoError(t, err)
		assert.False(t, other.IsDefault, "and no other probe is")

		var lastLogin *string
		require.NoError(t, db.NewSelect().Table("users").Column("last_login_at").Where("email = 'user@example.com'").Scan(ctx, &lastLogin))
		assert.Nil(t, lastLogin, "nobody has a last sign-in before they sign in")

		// the migration can be undone
		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT is_default FROM probes")
		assert.Error(t, err)
	})
}

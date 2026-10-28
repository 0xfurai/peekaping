package workspace_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/workspace"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

const apiKeyWorkspaceMigration = "20261102120000"

// storedAPIKey inserts a key as an earlier release stored it, with no workspace, and returns the
// token that opens it.
func storedAPIKey(t *testing.T, db *bun.DB, name string) string {
	t.Helper()
	id, secret := uuid.NewString(), "secret-"+uuid.NewString()
	payload, err := json.Marshal(map[string]string{"id": id, "key": secret})
	require.NoError(t, err)
	sum := sha256.Sum256([]byte(secret))
	_, err = db.Exec("INSERT INTO api_keys (id, name, key_hash, display_key) VALUES (?, ?, ?, 'pk_key...')", id, name, "sha256$"+hex.EncodeToString(sum[:]))
	require.NoError(t, err)
	return "pk_" + base64.URLEncoding.EncodeToString(payload)
}

func TestMigrations_TheKeysOfAnUpgradedInstallBelongToItsWorkspaceAndKeepWorking(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		first, second := storedAPIKey(t, db, "First"), storedAPIKey(t, db, "Second")

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		only := workspaces(t, db)
		require.Len(t, only, 1)
		service := api_key.NewService(api_key.NewSQLRepository(db), zap.NewNop().Sugar())
		for name, token := range map[string]string{"First": first, "Second": second} {
			key, err := service.ValidateKey(ctx, token)
			require.NoError(t, err, name)
			assert.Equal(t, name, key.Name)
			assert.Equal(t, workspace.ID(only[0].ID), key.WorkspaceID, name)
		}
	})
}

func TestMigrations_AKeyBelongsToNoWorkspaceOnAnInstallWithNone(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		// Records and no user: the upgrade still makes the workspace, so that the key has one
		storedAPIKey(t, db, "Orphan")

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		assert.Len(t, workspaces(t, db), 1)
		var count int
		require.NoError(t, db.NewSelect().Table("api_keys").Where("workspace_id = ?", workspaces(t, db)[0].ID).ColumnExpr("COUNT(*)").Scan(ctx, &count))
		assert.Equal(t, 1, count)
	})
}

func TestMigrations_TheAPIKeyWorkspaceGoesDownAndComesBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, apiKeyWorkspaceMigration)
		insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx) // only this migration is left to run
		require.NoError(t, err)
		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT workspace_id FROM api_keys")
		assert.Error(t, err, "the column goes")

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT workspace_id FROM api_keys")
		assert.NoError(t, err, "and comes back")
	})
}

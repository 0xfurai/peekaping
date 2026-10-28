package workspace_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The status pages of an install belong to a workspace (PP-150, docs/adr/0005 section 2).

const statusPageWorkspaceMigration = "20261105120000"

func TestMigrations_TheStatusPagesOfAnUpgradedInstallBelongToItsWorkspace(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		first, second := uuid.NewString(), uuid.NewString()
		for id, slug := range map[string]string{first: "first", second: "second"} {
			_, err := db.Exec("INSERT INTO status_pages (id, slug, title) VALUES (?, ?, ?)", id, slug, slug)
			require.NoError(t, err)
		}
		domainID := uuid.NewString()
		_, err := db.Exec("INSERT INTO domain_status_page (id, status_page_id, domain) VALUES (?, ?, 'status.example.com')", domainID, first)
		require.NoError(t, err)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		only := workspaces(t, db)
		require.Len(t, only, 1)
		var rows []struct {
			ID          string `bun:"id"`
			WorkspaceID string `bun:"workspace_id"`
		}
		require.NoError(t, db.NewSelect().Table("status_pages").Column("id", "workspace_id").Scan(ctx, &rows))
		owners := map[string]string{}
		for _, row := range rows {
			owners[row.ID] = row.WorkspaceID
		}
		assert.Equal(t, map[string]string{first: only[0].ID, second: only[0].ID}, owners)
		var domains int
		require.NoError(t, db.NewSelect().Table("domain_status_page").ColumnExpr("COUNT(*)").Where("status_page_id = ?", first).Scan(ctx, &domains))
		assert.Equal(t, 1, domains, "and the page keeps its domain")
	})
}

func TestMigrations_AStatusPageWithoutAWorkspaceIsRefused(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, statusPageWorkspaceMigration)
		insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO status_pages (id, slug, title) VALUES (?, 'no-workspace', 'No workspace')", uuid.NewString())
		assert.Error(t, err)
		_, err = db.Exec("INSERT INTO status_pages (id, workspace_id, slug, title) VALUES (?, ?, 'unknown', 'Unknown workspace')", uuid.NewString(), uuid.NewString())
		assert.Error(t, err, "nor does a workspace that doesn't exist do")
	})
}

func TestMigrations_TheStatusPageWorkspaceGoesDownAndComesBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, statusPageWorkspaceMigration)
		_, err := upgrade.Migrate(ctx) // only this migration is left to run
		require.NoError(t, err)
		workspaceID, pageID := uuid.NewString(), uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Kept')", workspaceID)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO status_pages (id, workspace_id, slug, title) VALUES (?, ?, 'kept', 'Kept')", pageID, workspaceID)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO domain_status_page (id, status_page_id, domain) VALUES (?, ?, 'kept.example.com')", uuid.NewString(), pageID)
		require.NoError(t, err)

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT workspace_id FROM status_pages")
		assert.Error(t, err, "the column goes")
		var slug string
		require.NoError(t, db.NewSelect().Table("status_pages").Column("slug").Where("id = ?", pageID).Scan(ctx, &slug), "and the page stays")
		assert.Equal(t, "kept", slug)
		var domains int
		require.NoError(t, db.NewSelect().Table("domain_status_page").ColumnExpr("COUNT(*)").Where("status_page_id = ?", pageID).Scan(ctx, &domains), "with its domain")
		assert.Equal(t, 1, domains)
		assert.True(t, indexExists(t, db, "idx_status_pages_slug"), "and the slug index stays")

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		var kept string
		require.NoError(t, db.NewSelect().Table("status_pages").Column("workspace_id").Where("id = ?", pageID).Scan(ctx, &kept), "and comes back")
		assert.Equal(t, workspaceID, kept, "with the page in the install's one workspace")
		// the slug stays unique across the install, whichever workspace holds it
		other := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Other')", other)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO status_pages (id, workspace_id, slug, title) VALUES (?, ?, 'kept', 'Taken')", uuid.NewString(), other)
		assert.Error(t, err)
	})
}

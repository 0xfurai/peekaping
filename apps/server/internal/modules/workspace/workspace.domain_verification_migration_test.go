package workspace_test

import (
	"context"
	"testing"

	"peekaping/internal/modules/domain_status_page"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Verifying custom domains by DNS (PP-165).

const domainVerificationMigration = "20261112120000"

func TestMigrations_TheDomainsOfAnUpgradedInstallAreUnverifiedAndGetATokenWhenFirstShown(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, domainVerificationMigration)
		_, err := upgrade.Migrate(ctx) // only this migration is left to run
		require.NoError(t, err)
		page := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Mine')", uuid.NewString())
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO status_pages (id, workspace_id, slug, title) VALUES (?, (SELECT id FROM workspaces LIMIT 1), 'mine', 'Mine')", page)
		require.NoError(t, err)

		// A row written by a version without verification, as the MongoDB import writes one: no new column
		_, err = db.Exec("INSERT INTO domain_status_page (id, status_page_id, domain) VALUES (?, ?, 'old.example.com')", uuid.NewString(), page)
		require.NoError(t, err)

		var row struct {
			Token      string  `bun:"verification_token"`
			VerifiedAt *string `bun:"verified_at"`
		}
		require.NoError(t, db.NewSelect().Table("domain_status_page").Column("verification_token", "verified_at").Scan(ctx, &row))
		assert.Empty(t, row.Token)
		assert.Nil(t, row.VerifiedAt, "an existing domain is not verified")

		svc := domain_status_page.NewService(domain_status_page.NewSQLRepository(db), zap.NewNop().Sugar())
		statuses, err := svc.Statuses(ctx, page)
		require.NoError(t, err)
		require.Len(t, statuses, 1)
		assert.Equal(t, "pending", statuses[0].State)
		assert.Regexp(t, "^peekaping-verify=[0-9a-f]{64}$", statuses[0].Records.TXT.Value)
	})
}

func TestMigrations_TheDomainVerificationColumnsGoDownAndComeBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, domainVerificationMigration)
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Kept')", uuid.NewString())
		require.NoError(t, err)
		page := uuid.NewString()
		_, err = db.Exec("INSERT INTO status_pages (id, workspace_id, slug, title) VALUES (?, (SELECT id FROM workspaces LIMIT 1), 'kept', 'Kept')", page)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO domain_status_page (id, status_page_id, domain) VALUES (?, ?, 'kept.example.com')", uuid.NewString(), page)
		require.NoError(t, err)

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT verification_token FROM domain_status_page")
		assert.Error(t, err, "the columns go")
		var domains int
		require.NoError(t, db.NewSelect().Table("domain_status_page").ColumnExpr("COUNT(*)").Scan(ctx, &domains), "and the domain stays")
		assert.Equal(t, 1, domains)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		_, err = db.Exec("SELECT verification_token, verified_at, pending_since, failing_since, last_checked_at FROM domain_status_page")
		assert.NoError(t, err, "and come back")
	})
}

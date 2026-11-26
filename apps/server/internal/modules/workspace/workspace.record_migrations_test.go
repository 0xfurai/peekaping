package workspace_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
)

// The notification channels, proxies, tags and maintenance windows of an install belong to a
// workspace, and tag names are unique within one (PP-149, docs/adr/0005 sections 2 and 6).

const (
	channelWorkspaceMigration  = "20261104120000"
	tagNamesPerWorkspaceMigrat = "20261104120400"
)

// recordTables are the tables PP-149 gives a workspace
var recordTables = []string{"notification_channels", "proxies", "tags", "maintenances"}

func TestMigrations_TheChannelsProxiesTagsAndMaintenancesOfAnUpgradedInstallBelongToItsWorkspace(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		ids := map[string]string{}
		for _, table := range recordTables {
			ids[table] = uuid.NewString()
			_, err := db.Exec(records[table], ids[table])
			require.NoError(t, err, table)
		}

		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		only := workspaces(t, db)
		require.Len(t, only, 1)
		for _, table := range recordTables {
			var workspaceID string
			require.NoError(t, db.NewSelect().Table(table).Column("workspace_id").Where("id = ?", ids[table]).Scan(ctx, &workspaceID), table)
			assert.Equal(t, only[0].ID, workspaceID, table)
		}
	})
}

func TestMigrations_ARecordWithoutAWorkspaceIsRefused(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)

		for _, table := range recordTables {
			_, err := db.Exec(records[table], uuid.NewString())
			assert.Error(t, err, table)
		}
	})
}

func TestMigrations_TheRecordWorkspacesGoDownAndComeBack(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, channelWorkspaceMigration)
		_, err := upgrade.Migrate(ctx) // the migrations of this ticket are the ones left to run
		require.NoError(t, err)
		workspaceID := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Kept')", workspaceID)
		require.NoError(t, err)
		ids := map[string]string{}
		for _, table := range recordTables {
			ids[table] = uuid.NewString()
			_, err := db.Exec(insertIn(table), ids[table], workspaceID)
			require.NoError(t, err, table)
		}

		_, err = upgrade.Rollback(ctx) // the migrations of this ticket ran as one group
		require.NoError(t, err)
		for _, table := range recordTables {
			_, err := db.Exec("SELECT workspace_id FROM " + table)
			assert.Error(t, err, table+": the column goes")
			var count int
			require.NoError(t, db.NewSelect().Table(table).ColumnExpr("COUNT(*)").Where("id = ?", ids[table]).Scan(ctx, &count), table)
			assert.Equal(t, 1, count, table+": and the record stays")
		}
		assert.False(t, indexExists(t, db, "idx_tags_workspace_name"))

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		for _, table := range recordTables {
			var kept string
			require.NoError(t, db.NewSelect().Table(table).Column("workspace_id").Where("id = ?", ids[table]).Scan(ctx, &kept), table)
			assert.Equal(t, workspaceID, kept, table+": and comes back in the install's one workspace")
		}
	})
}

// insertIn is the insert of a record into a workspace, with the ID and the workspace as its arguments
func insertIn(table string) string {
	statements := map[string]string{
		"notification_channels": `INSERT INTO notification_channels (id, workspace_id, name, type) VALUES (?, ?, 'Channel', 'webhook')`,
		"proxies":               `INSERT INTO proxies (id, workspace_id, protocol, host, port) VALUES (?, ?, 'http', 'proxy.example.com', 8080)`,
		"tags":                  `INSERT INTO tags (id, workspace_id, name) VALUES (?, ?, 'Tag')`,
		"maintenances":          `INSERT INTO maintenances (id, workspace_id, title, strategy) VALUES (?, ?, 'Window', 'manual')`,
	}
	return statements[table]
}

func insertTag(db *bun.DB, workspaceID, name string) error {
	_, err := db.Exec("INSERT INTO tags (id, workspace_id, name) VALUES (?, ?, ?)", uuid.NewString(), workspaceID, name)
	return err
}

func TestMigrations_TagNamesAreUniqueWithinAWorkspaceAndNotAcrossTheInstall(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		first := workspaces(t, db)[0].ID
		second := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Second')", second)
		require.NoError(t, err)

		require.NoError(t, insertTag(db, first, "production"))
		require.NoError(t, insertTag(db, second, "production"), "another workspace may have the same name")
		assert.Error(t, insertTag(db, first, "production"), "the same workspace may not")
		assert.Error(t, insertTag(db, second, "production"))
		require.NoError(t, insertTag(db, first, "Production"), "names stay case sensitive, as they were")
	})
}

func TestMigrations_TheTagsOfAnUpgradedInstallKeepTheirColumnsAndIndexes(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, workspacesMigration)
		insertUser(t, db, "first@example.com")
		tagID, monitorID := uuid.NewString(), storedMonitor(t, db, "Tagged")
		_, err := db.Exec("INSERT INTO tags (id, name, color, description) VALUES (?, 'production', '#ff0000', 'Live systems')", tagID)
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO monitor_tags (id, monitor_id, tag_id) VALUES (?, ?, ?)", uuid.NewString(), monitorID, tagID)
		require.NoError(t, err)

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)

		var tag struct {
			Name        string `bun:"name"`
			Color       string `bun:"color"`
			Description string `bun:"description"`
		}
		require.NoError(t, db.NewSelect().Table("tags").Column("name", "color", "description").Where("id = ?", tagID).Scan(ctx, &tag))
		assert.Equal(t, "production", tag.Name)
		assert.Equal(t, "#ff0000", tag.Color)
		assert.Equal(t, "Live systems", tag.Description)
		var links int
		require.NoError(t, db.NewSelect().Table("monitor_tags").ColumnExpr("COUNT(*)").Where("tag_id = ?", tagID).Scan(ctx, &links))
		assert.Equal(t, 1, links, "the monitor stays tagged")
		assert.True(t, indexExists(t, db, "idx_tags_name"))
		assert.True(t, indexExists(t, db, "idx_tags_workspace_id"))
		assert.True(t, indexExists(t, db, "idx_tags_workspace_name"))

		// deleting the tag still deletes its links, which the rebuilt table's key must keep doing
		_, err = db.Exec("DELETE FROM tags WHERE id = ?", tagID)
		require.NoError(t, err)
		require.NoError(t, db.NewSelect().Table("monitor_tags").ColumnExpr("COUNT(*)").Where("tag_id = ?", tagID).Scan(ctx, &links))
		assert.Zero(t, links)
	})
}

func TestMigrations_TagNamesAreUniqueAcrossTheInstallAgainWhenTheMigrationGoesDown(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, tagNamesPerWorkspaceMigrat)
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		workspaceID := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Only')", workspaceID)
		require.NoError(t, err)
		require.NoError(t, insertTag(db, workspaceID, "production"))

		_, err = upgrade.Rollback(ctx)
		require.NoError(t, err)

		assert.False(t, indexExists(t, db, "idx_tags_workspace_name"))
		other := uuid.NewString()
		_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, 'Other')", other)
		require.NoError(t, err)
		assert.Error(t, insertTag(db, other, "production"), "a name is taken across the install again")
		require.NoError(t, insertTag(db, other, "staging"))
		assert.True(t, indexExists(t, db, "idx_tags_name"))
		assert.True(t, indexExists(t, db, "idx_tags_workspace_id"))

		_, err = upgrade.Migrate(ctx)
		require.NoError(t, err)
		require.NoError(t, insertTag(db, other, "production"), "and per workspace once more")
	})
}

func TestMigrations_ATagNameTwoWorkspacesShareStopsTheRollbackWithoutChangingAnything(t *testing.T) {
	forEachUnmigratedBackend(t, func(t *testing.T, db *bun.DB) {
		ctx := context.Background()
		upgrade := migratedBefore(t, db, tagNamesPerWorkspaceMigrat)
		_, err := upgrade.Migrate(ctx)
		require.NoError(t, err)
		first, second := uuid.NewString(), uuid.NewString()
		for _, id := range []string{first, second} {
			_, err = db.Exec("INSERT INTO workspaces (id, name) VALUES (?, ?)", id, id)
			require.NoError(t, err)
			require.NoError(t, insertTag(db, id, "production"))
		}

		_, err = upgrade.Rollback(ctx)
		assert.Error(t, err)
		assert.True(t, indexExists(t, db, "idx_tags_workspace_name"), "the per workspace rule stays")
		var count int
		require.NoError(t, db.NewSelect().Table("tags").ColumnExpr("COUNT(*)").Scan(ctx, &count))
		assert.Equal(t, 2, count)
	})
}

// replaceTagsTable swaps the tags table, which is empty, for one with the name column as column
// says and the unique rule on it written as unique says. The upgrade has to find the rule in the schema and not in the way the DDL spells it.
func replaceTagsTable(t *testing.T, db *bun.DB, name, unique string) {
	t.Helper()
	_, err := db.Exec("DROP TABLE tags")
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE tags (
		id UUID PRIMARY KEY,
		` + name + `,
		color VARCHAR(7) NOT NULL DEFAULT '#3B82F6',
		description TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE` + unique + `)`)
	require.NoError(t, err)
}

func TestMigrations_TagNamesPerWorkspaceWhateverWayTheOldUniqueRuleIsWritten(t *testing.T) {
	for name, table := range map[string]struct{ column, unique string }{
		"a table constraint":                 {"name VARCHAR(100) NOT NULL", ", UNIQUE (name)"},
		"a quoted table constraint":          {`"name" VARCHAR(100) NOT NULL`, `, CONSTRAINT tags_name_unique UNIQUE ("name")`},
		"the column rule after NOT NULL":     {"name VARCHAR(100) NOT NULL UNIQUE", ""},
		"the column rule on another type":    {"name TEXT UNIQUE NOT NULL", ""},
		"the column rule on a lower case id": {"name varchar(100) unique not null", ""},
	} {
		t.Run(name, func(t *testing.T) {
			db := sqliteDB(t)
			ctx := context.Background()
			upgrade := migratedBefore(t, db, tagNamesPerWorkspaceMigrat)
			replaceTagsTable(t, db, table.column, table.unique)
			first, second := uuid.NewString(), uuid.NewString()
			for _, id := range []string{first, second} {
				_, err := db.Exec("INSERT INTO workspaces (id, name) VALUES (?, ?)", id, id)
				require.NoError(t, err)
			}
			require.NoError(t, insertTag(db, first, "production"))

			_, err := upgrade.Migrate(ctx)
			require.NoError(t, err)

			require.NoError(t, insertTag(db, second, "production"), "another workspace may have the same name")
			assert.Error(t, insertTag(db, first, "production"), "the same workspace may not")
		})
	}
}

func TestMigrations_ARuleOnTheTagNameAloneThatTheUpgradeCannotRemoveStopsIt(t *testing.T) {
	db := sqliteDB(t)
	ctx := context.Background()
	upgrade := migratedBefore(t, db, tagNamesPerWorkspaceMigrat)
	_, err := db.Exec("CREATE UNIQUE INDEX idx_tags_name_only ON tags (name)")
	require.NoError(t, err)

	_, err = upgrade.Migrate(ctx)

	require.Error(t, err, "the install keeps a rule the migration was to remove, and says so")
	assert.Contains(t, err.Error(), "tags")
	assert.False(t, indexExists(t, db, "idx_tags_workspace_name"), "and nothing is changed")
	assert.True(t, indexExists(t, db, "idx_tags_name_only"))
}

func TestMigrations_TagNamesPerWorkspaceWhateverTheOldPostgresConstraintIsCalled(t *testing.T) {
	for name, setup := range map[string]string{
		"a renamed constraint": "ALTER TABLE tags RENAME CONSTRAINT tags_name_key TO tags_name_renamed",
		"a unique index and no constraint": "ALTER TABLE tags DROP CONSTRAINT tags_name_key; " +
			"CREATE UNIQUE INDEX tags_name_only ON tags (name)",
	} {
		t.Run(name, func(t *testing.T) {
			db := postgresDB(t)
			ctx := context.Background()
			upgrade := migratedBefore(t, db, tagNamesPerWorkspaceMigrat)
			for _, statement := range strings.Split(setup, "; ") {
				_, err := db.Exec(statement)
				require.NoError(t, err)
			}
			first, second := uuid.NewString(), uuid.NewString()
			for _, id := range []string{first, second} {
				_, err := db.Exec("INSERT INTO workspaces (id, name) VALUES (?, ?)", id, id)
				require.NoError(t, err)
			}
			require.NoError(t, insertTag(db, first, "production"))

			_, err := upgrade.Migrate(ctx)
			require.NoError(t, err)

			require.NoError(t, insertTag(db, second, "production"), "another workspace may have the same name")
			assert.Error(t, insertTag(db, first, "production"), "the same workspace may not")
		})
	}
}

package mongoimport_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/workspace"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestRunRefusesATargetThatIsNeitherSQLiteNorPostgreSQL(t *testing.T) {
	sqldb, err := sql.Open("mysql", "u:p@tcp(127.0.0.1:1)/peekaping")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqldb.Close() })
	db := bun.NewDB(sqldb, mysqldialect.New())

	report, err := mongoimport.Run(context.Background(), db, mongoimport.Options{MongoURI: "mongodb://127.0.0.1:1"})

	assert.Nil(t, report)
	assert.EqualError(t, err, "the target must be a SQLite or PostgreSQL database, not mysql")
}

func TestRunSaysWhenItCantConnectToMongoDB(t *testing.T) {
	for name, uri := range map[string]string{
		"an address that is not a MongoDB URI": "http://127.0.0.1:1",
		"a server that is not there":           "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=300&connectTimeoutMS=300",
	} {
		t.Run(name, func(t *testing.T) {
			report, err := mongoimport.Run(context.Background(), sqliteTarget(t), mongoimport.Options{MongoURI: uri})

			assert.Nil(t, report)
			assert.ErrorContains(t, err, "connect to MongoDB: ")
		})
	}
}

// The migrations are the caller's to say, so that the import takes the same lock as "bun db migrate"
func TestRunNeedsAMigrateFunctionAndSaysWhenItFails(t *testing.T) {
	in := mongoimporttest.Empty(t)
	in.Insert(t, "tags", bson.D{{Key: "name", Value: "any"}})
	db := sqliteTarget(t)

	opts := in.Options()
	opts.Migrate = nil
	_, err := mongoimport.Run(context.Background(), db, opts)
	assert.EqualError(t, err, "mongoimport: Options.Migrate is required")

	opts.Migrate = func(context.Context, *bun.DB) error { return errors.New("the lock is held") }
	_, err = mongoimport.Run(context.Background(), db, opts)
	assert.EqualError(t, err, "apply the SQL migrations: the lock is held")
}

// A target that Peekaping wrote one setting to is told apart from one that wrote several
func TestImportNamesTheOneSettingTheTargetHadWritten(t *testing.T) {
	in := mongoimporttest.Seed(t)

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		firstStart(t, db)
		_, err := db.Exec(`DELETE FROM settings WHERE key <> 'only_on_the_target'`)
		require.NoError(t, err)
		require.Equal(t, 1, tableCounts(t, db, "settings")["settings"])

		report, err := mongoimport.Run(context.Background(), db, in.Options())

		require.NoError(t, err)
		require.NotEmpty(t, report.Notes)
		assert.Equal(t, "the setting that the target had written when Peekaping first started on it was replaced by the install's", report.Notes[0])
	})
}

// An install with no workspace has nothing to hold the settings that belong to one
func TestImportLeavesOutTheWorkspaceSettingsOfAnInstallWithNoWorkspace(t *testing.T) {
	in := mongoimporttest.Empty(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, doc := range []mongoimporttest.SettingDocument{
		{ID: primitive.NewObjectID(), Key: "custom", Value: "kept", Type: "string", CreatedAt: now, UpdatedAt: now},
		{ID: primitive.NewObjectID(), Key: setting.DataRetentionPeriodKey, Value: "30", Type: "int", CreatedAt: now, UpdatedAt: now},
		{ID: primitive.NewObjectID(), Key: workspace.CertExpiryNotifyDaysKey, Value: "[7]", Type: "json", CreatedAt: now, UpdatedAt: now},
	} {
		_, err := in.Collection("setting").InsertOne(ctx, doc)
		require.NoError(t, err)
	}
	// A document whose _id is not an ObjectID can't be given a UUID
	_, err := in.Collection("setting").InsertOne(ctx, bson.D{{Key: "_id", Value: "not-an-object-id"}, {Key: "key", Value: "stray"}, {Key: "value", Value: "x"}, {Key: "type", Value: "string"}})
	require.NoError(t, err)

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())

		require.NoError(t, err)
		r := collectionReport(t, report, "setting")
		assert.Equal(t, 4, r.Read)
		assert.Equal(t, 1, r.Copied)
		assert.Equal(t, map[string]int{"no ObjectID as _id": 1, "a workspace setting, and the install has no workspace": 2}, r.Skipped)
		assert.Equal(t, map[string]int{"settings": 1, "workspace_settings": 0, "workspaces": 0}, tableCounts(t, db, "settings", "workspace_settings", "workspaces"))
		assert.Empty(t, report.Notes, "the install has no workspace to keep a year of data for")
	})
}

// A status page names how many monitors are hidden on it, and which of its fields SQL doesn't keep
func TestImportCountsHiddenMonitorsAndUnusedFieldsPerStatusPage(t *testing.T) {
	in := mongoimporttest.Seed(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	// The shop page already hides one monitor
	_, err := in.Collection("monitor_status_page").InsertOne(ctx, mongoimporttest.MonitorStatusPageDocument{
		ID: primitive.NewObjectID(), StatusPageID: mustObjectID(t, in.ID("status_page", "shop")), MonitorID: mustObjectID(t, in.ID("monitor", "wide")),
		Order: 3, Active: false, CreatedAt: now, UpdatedAt: now,
	})
	require.NoError(t, err)
	_, err = in.Collection("status_pages").InsertOne(ctx, mongoimporttest.StatusPageDocument{
		ID: primitive.NewObjectID(), Slug: "locked", Title: "Locked", Theme: "light", AutoRefreshInterval: 300, CreatedAt: now, UpdatedAt: now, Password: "secret",
	})
	require.NoError(t, err)

	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(ctx, db, in.Options())

		require.NoError(t, err)
		assert.Equal(t, []string{
			`status page "shop": 2 monitors are hidden on the page (active is false) and will show (SQL has no column for it)`,
			`status page "shop": search_engine_index, password and google_analytics_tag_id are not kept (Peekaping does not use them)`,
			`status page "locked": password is not kept (Peekaping does not use them)`,
		}, report.Notes)
	})
}

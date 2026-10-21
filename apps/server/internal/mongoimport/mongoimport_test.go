package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/internal/legacyid"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"
	"peekaping/internal/modules/shared"
	"peekaping/internal/mongoimport"
	"peekaping/internal/mongoimport/mongoimporttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// TestImport copies one MongoDB install into each kind of SQL database and
// reads it back through the SQL repositories and services.
func TestImport(t *testing.T) {
	in := mongoimporttest.Seed(t)
	forEachTarget(t, func(t *testing.T, db *bun.DB) {
		report, err := mongoimport.Run(context.Background(), db, mongoimport.Options{MongoURI: in.URI, MongoDatabase: in.Database})
		require.NoError(t, err)
		t.Log("\n" + report.String())

		t.Run("accounts", func(t *testing.T) { checkAccounts(t, in, db, report) })
		t.Run("configuration", func(t *testing.T) { checkConfiguration(t, in, db, report) })
		t.Run("history", func(t *testing.T) { checkHistory(t, in, db, report) })
	})
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	id, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return id
}

// uuidOf is the UUID an ObjectID becomes. legacyid has its own test against UUIDs computed outside Go.
func uuidOf(t *testing.T, objectID string) string {
	t.Helper()
	id, ok := legacyid.FromObjectID(objectID)
	require.True(t, ok, objectID)
	return id
}

// collectionReport is the report's line for a MongoDB collection.
func collectionReport(t *testing.T, report *mongoimport.Report, collection string) *mongoimport.CollectionReport {
	t.Helper()
	for _, c := range report.Collections {
		if c.Collection == collection {
			return c
		}
	}
	require.Failf(t, "no report", "the report has no line for %s", collection)
	return nil
}

// assertSameTime checks two times are the same moment, to the millisecond MongoDB keeps.
func assertSameTime(t *testing.T, want, got time.Time, msgAndArgs ...any) {
	t.Helper()
	assert.WithinDuration(t, want, got, time.Millisecond, msgAndArgs...)
}

func checkAccounts(t *testing.T, in *mongoimporttest.Install, db *bun.DB, report *mongoimport.Report) {
	ctx := context.Background()

	t.Run("users", func(t *testing.T) {
		want, err := auth.NewMongoRepository(in.Client, in.Config).FindByEmail(ctx, mongoimporttest.AdminEmail)
		require.NoError(t, err)
		sqlUsers := auth.NewSQLRepository(db)
		got, err := sqlUsers.FindByEmail(ctx, mongoimporttest.AdminEmail)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, uuidOf(t, in.AdminID), got.ID)
		assert.Equal(t, want.Password, got.Password)
		assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(got.Password), []byte(mongoimporttest.AdminPassword)))
		assert.True(t, got.Active)
		assert.Equal(t, mongoimporttest.AdminTOTPSecret, got.TwoFASecret)
		assert.True(t, got.TwoFAStatus)
		assert.Equal(t, "123456", got.TwoFALastToken)
		assertSameTime(t, want.CreatedAt, got.CreatedAt)
		assertSameTime(t, want.UpdatedAt, got.UpdatedAt)

		// A user without createdAt and updatedAt was created when its ObjectID was made. The
		// document that came later with the same email can't have the email too.
		var second struct {
			ID primitive.ObjectID `bson:"_id"`
		}
		require.NoError(t, in.Collection("users").FindOne(ctx, bson.M{"email": "second@example.com"}).Decode(&second))
		got, err = sqlUsers.FindByEmail(ctx, "second@example.com")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, uuidOf(t, second.ID.Hex()), got.ID)
		assert.True(t, got.Active, "the first document of the email is the one that was copied")
		assert.True(t, got.CreatedAt.Equal(second.ID.Timestamp()), "%s is not %s", got.CreatedAt, second.ID.Timestamp())
		assert.True(t, got.UpdatedAt.Equal(got.CreatedAt))

		count, err := sqlUsers.FindAllCount(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 2, count)

		r := collectionReport(t, report, "users")
		assert.Equal(t, "users", r.Table)
		assert.Equal(t, 3, r.Read)
		assert.Equal(t, 2, r.Copied)
		assert.Equal(t, map[string]int{"email already used by an earlier user": 1}, r.Skipped)
		assert.Equal(t, map[string]int{
			"createdAt missing or zero: used the time the ObjectID was made": 1,
			"updatedAt missing or zero: used the creation time":              1,
		}, r.Fixed)
	})

	t.Run("settings", func(t *testing.T) {
		mongoSettings := setting.NewMongoRepository(in.Client, in.Config)
		sqlSettings := setting.NewSQLRepository(db)
		// The JWT signing secrets come across, or every session and refresh token would stop working
		for _, key := range []string{shared.AccessTokenSecretKey, shared.RefreshTokenSecretKey, "ACCESS_TOKEN_EXPIRED_IN", "REFRESH_TOKEN_EXPIRED_IN", "cert_expiry_notify_days"} {
			want, err := mongoSettings.GetByKey(ctx, key)
			require.NoError(t, err)
			require.NotNil(t, want, key)
			got, err := sqlSettings.GetByKey(ctx, key)
			require.NoError(t, err)
			require.NotNil(t, got, key)
			assert.Equal(t, want.Value, got.Value, key)
			assert.Equal(t, want.Type, got.Type, key)
			assertSameTime(t, want.UpdatedAt, got.UpdatedAt, key)
			assert.False(t, got.CreatedAt.IsZero(), key)
		}
		assert.NotEmpty(t, mustSetting(t, sqlSettings, shared.AccessTokenSecretKey).Value)

		// The retention setting has two documents, and the one saved last wins
		retention := mustSetting(t, sqlSettings, setting.DataRetentionPeriodKey)
		assert.Equal(t, mongoimporttest.RetentionDays, retention.Value)
		assert.Equal(t, "int", retention.Type)

		r := collectionReport(t, report, "setting")
		assert.Equal(t, "settings", r.Table)
		assert.Equal(t, 7, r.Read)
		assert.Equal(t, 6, r.Copied)
		assert.Equal(t, map[string]int{"an older document of the same key": 1}, r.Skipped)
		// A setting is upserted, which never sets created_at
		assert.Equal(t, 6, r.Fixed["created_at missing or zero: used the time the ObjectID was made"])
	})

	t.Run("api keys", func(t *testing.T) {
		mongoKeys, err := api_key.NewMongoRepository(in.Client, in.Config).FindAll(ctx)
		require.NoError(t, err)
		require.Len(t, mongoKeys, 2)
		sqlKeys := api_key.NewSQLRepository(db)
		for _, want := range mongoKeys {
			got, err := sqlKeys.FindByID(ctx, uuidOf(t, want.ID))
			require.NoError(t, err)
			require.NotNil(t, got, want.Name)
			assert.Equal(t, want.Name, got.Name)
			assert.Equal(t, want.KeyHash, got.KeyHash)
			assert.Equal(t, want.DisplayKey, got.DisplayKey)
			assert.Equal(t, want.UsageCount, got.UsageCount)
			assert.Equal(t, want.MaxUsageCount, got.MaxUsageCount)
			assertSameTimePtr(t, want.LastUsed, got.LastUsed)
			assertSameTimePtr(t, want.ExpiresAt, got.ExpiresAt)
			assertSameTime(t, want.CreatedAt, got.CreatedAt)
			assertSameTime(t, want.UpdatedAt, got.UpdatedAt)
		}
		used, err := sqlKeys.FindByID(ctx, uuidOf(t, in.APIKeyID))
		require.NoError(t, err)
		assert.Equal(t, "CI", used.Name)
		assert.EqualValues(t, 1, used.UsageCount)
		assert.NotNil(t, used.LastUsed)

		// The token the install handed out still authenticates, though it carries the ObjectID
		service := api_key.NewService(sqlKeys, zap.NewNop().Sugar())
		validated, err := service.ValidateKey(ctx, in.APIKeyToken)
		require.NoError(t, err)
		assert.Equal(t, uuidOf(t, in.APIKeyID), validated.ID)

		r := collectionReport(t, report, "api_keys")
		assert.Equal(t, "api_keys", r.Table)
		assert.Equal(t, 2, r.Read)
		assert.Equal(t, 2, r.Copied)
		assert.Zero(t, r.SkippedTotal())
	})
}

func mustSetting(t *testing.T, repo setting.Repository, key string) *setting.Model {
	t.Helper()
	got, err := repo.GetByKey(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, got, key)
	return got
}

func assertSameTimePtr(t *testing.T, want, got *time.Time) {
	t.Helper()
	if want == nil {
		assert.Nil(t, got)
		return
	}
	if assert.NotNil(t, got) {
		assertSameTime(t, *want, *got)
	}
}

// tableCounts is how many rows each table of the database holds.
func tableCounts(t *testing.T, db *bun.DB, tables ...string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range tables {
		n, err := db.NewSelect().TableExpr("?", bun.Ident(table)).Count(context.Background())
		require.NoError(t, err, table)
		counts[table] = n
	}
	return counts
}

func TestImportRefusesATargetThatHoldsData(t *testing.T) {
	in := mongoimporttest.Seed(t)
	ctx := context.Background()
	opts := mongoimport.Options{MongoURI: in.URI, MongoDatabase: in.Database}
	tables := []string{"users", "settings", "api_keys"}

	t.Run("an imported install", func(t *testing.T) {
		forEachTarget(t, func(t *testing.T, db *bun.DB) {
			_, err := mongoimport.Run(ctx, db, opts)
			require.NoError(t, err)
			before := tableCounts(t, db, tables...)
			require.Equal(t, map[string]int{"users": 2, "settings": 6, "api_keys": 2}, before)

			// Importing again would double every row, so it changes nothing and says why
			_, err = mongoimport.Run(ctx, db, opts)
			assert.ErrorIs(t, err, mongoimport.ErrTargetNotEmpty)
			assert.ErrorContains(t, err, "users, settings, api_keys")
			assert.Equal(t, before, tableCounts(t, db, tables...))
		})
	})

	// A Peekaping that was started once has written its settings, which is data too. It may be an
	// older one that hasn't run the latest migrations, which the import leaves alone.
	t.Run("a database with a setting and no migrations", func(t *testing.T) {
		forEachTarget(t, func(t *testing.T, db *bun.DB) {
			_, err := db.Exec(`CREATE TABLE settings (key VARCHAR(255) PRIMARY KEY, value TEXT NOT NULL, type VARCHAR(50) NOT NULL)`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO settings (key, value, type) VALUES ('KEEP_DATA_PERIOD_DAYS', '90', 'int')`)
			require.NoError(t, err)

			_, err = mongoimport.Run(ctx, db, opts)
			assert.ErrorIs(t, err, mongoimport.ErrTargetNotEmpty)
			assert.ErrorContains(t, err, "settings")
			assert.Equal(t, map[string]int{"settings": 1}, tableCounts(t, db, "settings"))
			_, err = db.Exec("SELECT 1 FROM monitors")
			assert.Error(t, err, "the migrations ran")
		})
	})
}

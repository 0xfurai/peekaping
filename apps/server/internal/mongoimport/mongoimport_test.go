package mongoimport_test

import (
	"context"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
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
	"github.com/uptrace/bun/migrate"
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
		report, err := mongoimport.Run(context.Background(), db, in.Options())
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
		want := mongoimporttest.Get[mongoimporttest.UserDocument](t, in, "users", bson.M{"email": mongoimporttest.AdminEmail})
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
		sqlSettings := setting.NewSQLRepository(db)
		// The JWT signing secrets come across, or every session and refresh token would stop working
		for _, key := range []string{shared.AccessTokenSecretKey, shared.RefreshTokenSecretKey, "ACCESS_TOKEN_EXPIRED_IN", "REFRESH_TOKEN_EXPIRED_IN", "cert_expiry_notify_days"} {
			want := mongoimporttest.Get[mongoimporttest.SettingDocument](t, in, "setting", bson.M{"key": key})
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
		mongoKeys := mongoimporttest.List[mongoimporttest.APIKeyDocument](t, in, "api_keys", bson.M{}, nil)
		require.Len(t, mongoKeys, 2)
		sqlKeys := api_key.NewSQLRepository(db)
		for _, want := range mongoKeys {
			got, err := sqlKeys.FindByID(ctx, uuidOf(t, want.ID.Hex()))
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
	opts := in.Options()
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
			assert.ErrorContains(t, err, "users, api_keys")
			assert.Equal(t, before, tableCounts(t, db, tables...))
		})
	})

	// A target a Peekaping started on once holds its default settings and nothing else: the first
	// start writes only those. They are replaced by the install's, the JWT signing secrets included.
	t.Run("a database after a first start", func(t *testing.T) {
		forEachTarget(t, func(t *testing.T, db *bun.DB) {
			firstStart(t, db)
			started := mustSetting(t, setting.NewSQLRepository(db), shared.AccessTokenSecretKey)
			require.NotEmpty(t, started.Value)

			report, err := mongoimport.Run(ctx, db, opts)
			require.NoError(t, err)

			settings := setting.NewSQLRepository(db)
			for _, key := range []string{shared.AccessTokenSecretKey, shared.RefreshTokenSecretKey, setting.DataRetentionPeriodKey} {
				want := mongoimporttest.Get[mongoimporttest.SettingDocument](t, in, "setting", bson.M{"key": key})
				assert.Equal(t, want.Value, mustSetting(t, settings, key).Value, key)
			}
			assert.NotEqual(t, started.Value, mustSetting(t, settings, shared.AccessTokenSecretKey).Value, "the target's own signing secret is still there")
			assert.Equal(t, mongoimporttest.RetentionDays, mustSetting(t, settings, setting.DataRetentionPeriodKey).Value, "the target's own 180 days are still there")
			gone, err := settings.GetByKey(ctx, "only_on_the_target")
			require.NoError(t, err)
			assert.Nil(t, gone, "a setting only the target had is still there")
			assert.Equal(t, map[string]int{"users": 2, "settings": 6, "api_keys": 2}, tableCounts(t, db, tables...))
			require.NotEmpty(t, report.Notes)
			assert.Equal(t, "the 7 settings that the target had written when Peekaping first started on it were replaced by the install's", report.Notes[0])
		})
	})

	// One user is the target's own data, which the import must not mix with another install's
	t.Run("a database with a user", func(t *testing.T) {
		forEachTarget(t, func(t *testing.T, db *bun.DB) {
			firstStart(t, db)
			_, err := db.Exec(`INSERT INTO users (id, email, password) VALUES ('11111111-1111-4111-8111-111111111111', 'me@example.com', 'hash')`)
			require.NoError(t, err)
			started := mustSetting(t, setting.NewSQLRepository(db), shared.AccessTokenSecretKey)

			_, err = mongoimport.Run(ctx, db, opts)
			assert.ErrorIs(t, err, mongoimport.ErrTargetNotEmpty)
			assert.ErrorContains(t, err, "users have rows")
			assert.Equal(t, map[string]int{"users": 1, "settings": 7, "api_keys": 0}, tableCounts(t, db, tables...))
			assert.Equal(t, started.Value, mustSetting(t, setting.NewSQLRepository(db), shared.AccessTokenSecretKey).Value, "the settings were replaced")
		})
	})

	// Not even the migrations run on a database that holds data, which may be an older one
	t.Run("a database with a user and no migrations", func(t *testing.T) {
		forEachTarget(t, func(t *testing.T, db *bun.DB) {
			_, err := db.Exec(`CREATE TABLE users (id VARCHAR(36) PRIMARY KEY, email VARCHAR(255) NOT NULL)`)
			require.NoError(t, err)
			_, err = db.Exec(`INSERT INTO users (id, email) VALUES ('11111111-1111-4111-8111-111111111111', 'me@example.com')`)
			require.NoError(t, err)

			_, err = mongoimport.Run(ctx, db, opts)
			assert.ErrorIs(t, err, mongoimport.ErrTargetNotEmpty)
			assert.Equal(t, map[string]int{"users": 1}, tableCounts(t, db, "users"))
			_, err = db.Exec("SELECT 1 FROM monitors")
			assert.Error(t, err, "the migrations ran")
		})
	})
}

// firstStart does what Peekaping's first start does to an empty database: migrates it and
// writes the default settings, the data retention period of a new install, and a setting of
// its own that the install has no counterpart for.
func firstStart(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	migrator := migrate.NewMigrator(db, migrations.Migrations)
	require.NoError(t, migrator.Init(ctx))
	_, err := migrator.Migrate(ctx)
	require.NoError(t, err)
	settings := setting.NewService(setting.NewSQLRepository(db), zap.NewNop().Sugar())
	require.NoError(t, settings.InitializeSettings(ctx))
	_, err = settings.SetByKey(ctx, setting.DataRetentionPeriodKey, &setting.Model{Value: "180", Type: "int"})
	require.NoError(t, err)
	_, err = settings.SetByKey(ctx, "only_on_the_target", &setting.Model{Value: "x", Type: "string"})
	require.NoError(t, err)
}

// Package mongoimporttest fills a MongoDB database the way a Peekaping install
// fills one, for the tests of the import into SQL. The import tests and the API
// tests that run on an imported database share it. It writes the documents itself, in the
// frozen storage format of documents.go, and doesn't use the modules' MongoDB repositories.
package mongoimporttest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"peekaping/cmd/bun/migrations"
	"peekaping/internal/mongoimport"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

const (
	AdminEmail      = "admin@example.com"
	AdminPassword   = "Correct-horse-1"
	AdminTOTPSecret = "JBSWY3DPEHPK3PXP"

	// RetentionDays is the data retention period the install is set to.
	RetentionDays = "30"
)

// Install is a MongoDB database holding a Peekaping install, and what the test
// put in it.
type Install struct {
	// URI and Database say where the install is. The database is dropped when the test ends.
	URI      string
	Database string

	Client *mongo.Client

	// AdminID is the ObjectID of the admin, who has two-factor authentication on.
	AdminID string
	// APIKeyID and APIKeyToken are the ObjectID of the API key named "CI" and the token that
	// authenticates with it, as the MongoDB install handed it out.
	APIKeyID    string
	APIKeyToken string

	ids map[string]string
}

// Seed fills a new database with an install, or skips the test when
// PEE_TEST_MONGO_URI doesn't name a MongoDB server.
func Seed(t *testing.T) *Install {
	t.Helper()
	in := Empty(t)
	in.seedAccounts(t)
	in.seedConfiguration(t)
	in.seedHistory(t)
	return in
}

// Empty is a new database with nothing in it, for a test that writes the few raw documents it
// needs. It skips the test when PEE_TEST_MONGO_URI doesn't name a MongoDB server.
func Empty(t *testing.T) *Install {
	t.Helper()
	uri := os.Getenv("PEE_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set PEE_TEST_MONGO_URI to run the import from MongoDB")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	require.NoError(t, err)
	require.NoError(t, client.Ping(ctx, nil))
	in := &Install{
		URI:      uri,
		Database: "pp227_" + primitive.NewObjectID().Hex(),
		Client:   client,
	}
	t.Cleanup(func() {
		require.NoError(t, client.Database(in.Database).Drop(context.Background()))
		require.NoError(t, client.Disconnect(context.Background()))
	})
	return in
}

// Options are the import options for the install, with a migrate that takes no lock: a test
// runs one migrate at a time, where "bun db import-mongo" takes the lock of "bun db migrate".
func (in *Install) Options() mongoimport.Options {
	return mongoimport.Options{
		MongoURI:      in.URI,
		MongoDatabase: in.Database,
		Migrate: func(ctx context.Context, db *bun.DB) error {
			migrator := migrate.NewMigrator(db, migrations.Migrations, migrate.WithMarkAppliedOnSuccess(true))
			if err := migrator.Init(ctx); err != nil {
				return err
			}
			_, err := migrator.Migrate(ctx)
			return err
		},
	}
}

// Collection is a collection of the install, for the documents that the seed has no helper for.
func (in *Install) Collection(name string) *mongo.Collection {
	return in.Client.Database(in.Database).Collection(name)
}

// Insert writes a raw document and returns its ObjectID.
func (in *Install) Insert(t *testing.T, collection string, document bson.D) primitive.ObjectID {
	t.Helper()
	return in.insert(t, collection, document)
}

// insert writes a raw document under a new ObjectID and returns it.
func (in *Install) insert(t *testing.T, collection string, document bson.D) primitive.ObjectID {
	t.Helper()
	id := primitive.NewObjectID()
	_, err := in.Collection(collection).InsertOne(context.Background(), append(bson.D{{Key: "_id", Value: id}}, document...))
	require.NoError(t, err)
	return id
}

// put writes a document of the frozen format.
func (in *Install) put(t *testing.T, collection string, document any) {
	t.Helper()
	_, err := in.Collection(collection).InsertOne(context.Background(), document)
	require.NoError(t, err)
}

// set changes keys of a document, as an update of the module's repository did.
func (in *Install) set(t *testing.T, collection string, id primitive.ObjectID, keys bson.M) {
	t.Helper()
	_, err := in.Collection(collection).UpdateOne(context.Background(), bson.M{"_id": id}, bson.M{"$set": keys})
	require.NoError(t, err)
}

// seedNow is the time that stamps a document a running install makes as it runs, cut to the
// millisecond MongoDB keeps.
func seedNow() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// The keys of the settings the install keeps. They are copies of the keys the server reads them
// under, because an install already holds them under these.
const (
	accessTokenSecretKey  = "auth.access_token_secret"
	refreshTokenSecretKey = "auth.refresh_token_secret"
	retentionPeriodKey    = "KEEP_DATA_PERIOD_DAYS"
)

func (in *Install) seedAccounts(t *testing.T) {
	t.Helper()
	now := seedNow()

	// The admin, with two-factor authentication on
	hash, err := bcrypt.GenerateFromPassword([]byte(AdminPassword), bcrypt.MinCost)
	require.NoError(t, err)
	admin := primitive.NewObjectID()
	in.put(t, "users", UserDocument{
		ID: admin, Email: AdminEmail, Password: string(hash), Active: true,
		TwoFASecret: AdminTOTPSecret, TwoFAStatus: true, TwoFALastToken: "123456", CreatedAt: now, UpdatedAt: now,
	})
	in.AdminID = admin.Hex()

	// A user from before the createdAt and updatedAt keys, and a second document for its email
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: string(hash)}, {Key: "active", Value: true}})
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: "other"}, {Key: "active", Value: false}})

	// The settings a first start writes, which never set created_at, and the one an operator changes.
	// The retention setting also has an older document of the same key.
	for _, s := range []struct{ key, value, kind string }{
		{"ACCESS_TOKEN_EXPIRED_IN", "15m", "string"},
		{"REFRESH_TOKEN_EXPIRED_IN", "720h", "string"},
		{accessTokenSecretKey, randomHex(t, 32), "string"},
		{refreshTokenSecretKey, randomHex(t, 32), "string"},
		{"cert_expiry_notify_days", "[7,14,21]", "json"},
		{retentionPeriodKey, RetentionDays, "int"},
	} {
		in.put(t, "setting", SettingDocument{ID: primitive.NewObjectID(), Key: s.key, Value: s.value, Type: s.kind, UpdatedAt: now})
	}
	in.put(t, "setting", SettingDocument{ID: primitive.NewObjectID(), Key: retentionPeriodKey, Value: "7", Type: "int", UpdatedAt: now.Add(-48 * time.Hour)})

	// API keys: one that has been used, and one with an expiry and a usage limit
	used, token := newAPIKey(t, "CI", nil, nil, now)
	used.UsageCount, used.LastUsed = 1, &now
	in.putAPIKey(t, used, now)
	in.APIKeyID, in.APIKeyToken = used.ID.Hex(), token
	expires, limit := now.Add(72*time.Hour), int64(100)
	dashboard, _ := newAPIKey(t, "Dashboard", &expires, &limit, now)
	in.putAPIKey(t, dashboard, now)
}

// putAPIKey writes an API key as the service leaves it. The service creates the key with an empty hash
// and then fills the hash in, and that writes updated_at, next to the model's updatedAt, so a key has both.
func (in *Install) putAPIKey(t *testing.T, key APIKeyDocument, hashedAt time.Time) {
	t.Helper()
	in.put(t, "api_keys", key)
	in.set(t, "api_keys", key.ID, bson.M{"updated_at": hashedAt})
}

// randomHex is n random bytes in hex, as the settings service makes a signing secret.
func randomHex(t *testing.T, n int) string {
	t.Helper()
	random := make([]byte, n)
	_, err := rand.Read(random)
	require.NoError(t, err)
	return hex.EncodeToString(random)
}

// newAPIKey is a new API key and the token that authenticates with it, made the way the API key
// service makes them. The token is "pk_" and the base64 of a JSON object with the key's ObjectID and
// a random secret. Only the SHA-256 hash of the secret is stored, as "sha256$" and the hex digest,
// with a display form of the token that keeps its first 8 and last 4 characters.
func newAPIKey(t *testing.T, name string, expiresAt *time.Time, maxUsageCount *int64, now time.Time) (APIKeyDocument, string) {
	t.Helper()
	id := primitive.NewObjectID()
	random := make([]byte, 32)
	_, err := rand.Read(random)
	require.NoError(t, err)
	secret := base64.URLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(secret))
	payload, err := json.Marshal(map[string]string{"id": id.Hex(), "key": secret})
	require.NoError(t, err)
	token := "pk_" + base64.URLEncoding.EncodeToString(payload)
	return APIKeyDocument{
		ID: id, Name: name, KeyHash: "sha256$" + hex.EncodeToString(sum[:]), DisplayKey: token[:8] + "..." + token[len(token)-4:],
		ExpiresAt: expiresAt, MaxUsageCount: maxUsageCount, CreatedAt: now, UpdatedAt: now,
	}, token
}

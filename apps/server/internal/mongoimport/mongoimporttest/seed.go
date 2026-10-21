// Package mongoimporttest fills a MongoDB database the way a Peekaping install
// fills one, for the tests of the import into SQL. The import tests and the API
// tests that run on an imported database share it.
package mongoimporttest

import (
	"context"
	"os"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/modules/api_key"
	"peekaping/internal/modules/auth"
	"peekaping/internal/modules/setting"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
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
	// Config names the database for the modules' Mongo repositories.
	Config *config.Config
	Logger *zap.SugaredLogger

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
		Logger:   zap.NewNop().Sugar(),
	}
	in.Config = &config.Config{DBName: in.Database}
	t.Cleanup(func() {
		require.NoError(t, client.Database(in.Database).Drop(context.Background()))
		require.NoError(t, client.Disconnect(context.Background()))
	})
	return in
}

// Collection is a collection of the install, for the raw documents that no repository writes.
func (in *Install) Collection(name string) *mongo.Collection {
	return in.Client.Database(in.Database).Collection(name)
}

// Insert writes a raw document and returns its ObjectID.
func (in *Install) Insert(t *testing.T, collection string, document bson.D) primitive.ObjectID {
	t.Helper()
	return in.insert(t, collection, document)
}

// insert writes a raw document and returns its ObjectID.
func (in *Install) insert(t *testing.T, collection string, document bson.D) primitive.ObjectID {
	t.Helper()
	id := primitive.NewObjectID()
	_, err := in.Collection(collection).InsertOne(context.Background(), append(bson.D{{Key: "_id", Value: id}}, document...))
	require.NoError(t, err)
	return id
}

func (in *Install) seedAccounts(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	// The admin, with two-factor authentication on
	hash, err := bcrypt.GenerateFromPassword([]byte(AdminPassword), bcrypt.MinCost)
	require.NoError(t, err)
	admin, err := auth.NewMongoRepository(in.Client, in.Config).Create(ctx, &auth.Model{
		Email: AdminEmail, Password: string(hash), Active: true,
		TwoFASecret: AdminTOTPSecret, TwoFAStatus: true, TwoFALastToken: "123456",
	})
	require.NoError(t, err)
	in.AdminID = admin.ID

	// A user from before the createdAt and updatedAt keys, and a second document for its email
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: string(hash)}, {Key: "active", Value: true}})
	in.insert(t, "users", bson.D{{Key: "email", Value: "second@example.com"}, {Key: "password", Value: "other"}, {Key: "active", Value: false}})

	// The settings a first start writes, which never set created_at, and the one an operator changes.
	// The retention setting also has an older document of the same key.
	settings := setting.NewService(setting.NewMongoRepository(in.Client, in.Config), in.Logger)
	require.NoError(t, settings.InitializeSettings(ctx))
	_, err = settings.SetByKey(ctx, setting.DataRetentionPeriodKey, &setting.Model{Value: RetentionDays, Type: "int"})
	require.NoError(t, err)
	in.insert(t, "setting", bson.D{
		{Key: "key", Value: setting.DataRetentionPeriodKey}, {Key: "value", Value: "7"}, {Key: "type", Value: "int"},
		{Key: "updated_at", Value: time.Now().Add(-48 * time.Hour).UTC()},
	})

	// API keys: one that has been used, and one with an expiry and a usage limit
	keys := api_key.NewService(api_key.NewMongoRepository(in.Client, in.Config), in.Logger)
	used, err := keys.Create(ctx, &api_key.CreateRequest{Name: "CI"})
	require.NoError(t, err)
	_, err = keys.ValidateKey(ctx, used.Token)
	require.NoError(t, err)
	in.APIKeyID, in.APIKeyToken = used.ID, used.Token
	expires, limit := time.Now().Add(72*time.Hour).UTC().Truncate(time.Millisecond), int64(100)
	_, err = keys.Create(ctx, &api_key.CreateRequest{Name: "Dashboard", ExpiresAt: &expires, MaxUsageCount: &limit})
	require.NoError(t, err)
}

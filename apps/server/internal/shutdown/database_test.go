package shutdown_test

import (
	"context"
	"testing"

	"peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/dig"
)

// resolveDatabase gets the Database a binary would from its container
func resolveDatabase(t *testing.T, constructor any) shutdown.Database {
	t.Helper()
	container := dig.New()
	if constructor != nil {
		require.NoError(t, container.Provide(constructor))
	}
	var database shutdown.Database
	require.NoError(t, container.Invoke(func(resolved shutdown.Database) { database = resolved }))
	return database
}

func TestDatabase_ClosesTheSQLDatabaseTheContainerProvides(t *testing.T) {
	logger, logs := observedLogger()
	db := openSQLite(t)
	queries := &recordQueries{}
	db.AddQueryHook(queries)
	database := resolveDatabase(t, func() *bun.DB { return db })

	require.NoError(t, database.Close("sqlite", logger))

	assert.Contains(t, queries.queries, "PRAGMA wal_checkpoint(TRUNCATE)")
	assert.Error(t, db.Ping(), "the database is still open")
	assert.Equal(t, 1, logs.FilterMessage("SQLite database closed gracefully").Len())
}

func TestDatabase_DisconnectsTheMongoClientTheContainerProvides(t *testing.T) {
	logger, logs := observedLogger()
	// The client connects lazily, so no server needs to run
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	require.NoError(t, err)
	database := resolveDatabase(t, func() *mongo.Client { return client })

	require.NoError(t, database.Close("mongo", logger))

	assert.ErrorIs(t, client.Ping(context.Background(), nil), mongo.ErrClientDisconnected, "the client is still connected")
	assert.Equal(t, 1, logs.FilterMessage("MongoDB client disconnected gracefully").Len())
}

// The worker has no database, and a binary is not told which storage backend it runs on when
// it provides none
func TestDatabase_ClosesNothingWhenTheContainerProvidesNoDatabase(t *testing.T) {
	logger, logs := observedLogger()
	database := resolveDatabase(t, nil)

	require.NoError(t, database.Close("", logger))

	assert.Zero(t, logs.Len())
}

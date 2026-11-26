package shutdown_test

import (
	"context"
	"testing"

	"peekaping/internal/shutdown"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"go.uber.org/dig"
)

// resolveDatabase gets the Database a binary would from its container
func resolveDatabase(t *testing.T, constructor any) shutdown.Database {
	t.Helper()
	container := dig.New()
	require.NoError(t, container.Provide(constructor))
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

	require.NoError(t, database.Close(context.Background(), "sqlite", logger))

	assert.Contains(t, queries.queries, "PRAGMA wal_checkpoint(TRUNCATE)")
	assert.Error(t, db.Ping(), "the database is still open")
	assert.Equal(t, 1, logs.FilterMessage("SQLite database closed gracefully").Len())
}

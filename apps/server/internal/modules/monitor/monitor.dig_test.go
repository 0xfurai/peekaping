package monitor_test

import (
	"context"
	"database/sql"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/modules/monitor"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/dig"
)

// transactorOf registers the monitor module for dbType over db and returns the Transactor it
// provides, the one the monitor service will write a monitor and its links with.
func transactorOf(t *testing.T, dbType string, db *bun.DB) dbtx.Transactor {
	t.Helper()
	cfg := &config.Config{DBType: dbType}
	container := dig.New()
	require.NoError(t, container.Provide(func() *config.Config { return cfg }))
	require.NoError(t, container.Provide(func() *bun.DB { return db }))
	require.NoError(t, monitor.RegisterDependencies(container))

	var transactor dbtx.Transactor
	require.NoError(t, container.Invoke(func(provided dbtx.Transactor) { transactor = provided }))
	return transactor
}

// insideTx reports whether the ctx WithTx gives fn carries a transaction.
func insideTx(t *testing.T, transactor dbtx.Transactor, db *bun.DB) bool {
	t.Helper()
	var inside bool
	require.NoError(t, transactor.WithTx(context.Background(), func(ctx context.Context) error {
		_, inside = dbtx.DB(ctx, db).(bun.Tx)
		return nil
	}))
	return inside
}

func TestRegisterDependencies_ProvidesTheTransactorOfTheStorageBackend(t *testing.T) {
	sqldb, err := sql.Open(sqliteshim.ShimName, "file::memory:")
	require.NoError(t, err)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	for _, dbType := range []string{"sqlite", "postgres", "postgresql", "mysql"} {
		t.Run(dbType, func(t *testing.T) {
			assert.True(t, insideTx(t, transactorOf(t, dbType, db), db), "a SQL storage backend gets a transaction")
		})
	}
}

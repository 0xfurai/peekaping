package dbtx_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peekaping/internal/config"
	"peekaping/internal/dbtx"
	"peekaping/internal/infra"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

// item is the one table the tests write to.
type item struct {
	bun.BaseModel `bun:"table:dbtx_items"`

	ID string `bun:"id,pk"`
}

// sqliteDB opens a SQLite database the way the app does: one connection, so a statement that
// doesn't run on the open transaction waits for a connection the transaction holds.
func sqliteDB(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "dbtx.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresDB opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresDB(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the PostgreSQL tests")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "dbtx_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema))
	require.NoError(t, err)
	connector := pgdriver.NewConnector(pgdriver.WithDSN(dsn), pgdriver.WithConnParams(map[string]any{"search_path": schema}))
	db := bun.NewDB(sql.OpenDB(connector), pgdialect.New())
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := admin.Exec("DROP SCHEMA ? CASCADE", bun.Ident(schema))
		require.NoError(t, err)
		require.NoError(t, admin.Close())
	})
	return db
}

// forEachSQLBackend runs fn on SQLite and, when PEE_TEST_POSTGRES_DSN is set, on PostgreSQL,
// with the items table created. The context fails the test's wait after a few seconds
// instead of hanging it.
func forEachSQLBackend(t *testing.T, fn func(t *testing.T, ctx context.Context, db *bun.DB)) {
	run := func(open func(*testing.T) *bun.DB) func(*testing.T) {
		return func(t *testing.T) {
			db := open(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			t.Cleanup(cancel)
			_, err := db.NewCreateTable().Model((*item)(nil)).Exec(ctx)
			require.NoError(t, err)
			fn(t, ctx, db)
		}
	}
	t.Run("SQLite", run(sqliteDB))
	t.Run("Postgres", run(postgresDB))
}

// save writes an item through whatever handle ctx selects for db.
func save(ctx context.Context, db *bun.DB, id string) error {
	_, err := dbtx.DB(ctx, db).NewInsert().Model(&item{ID: id}).Exec(ctx)
	return err
}

// stored lists the committed item IDs.
func stored(t *testing.T, ctx context.Context, db *bun.DB) []string {
	t.Helper()
	ids := []string{}
	require.NoError(t, db.NewSelect().Model((*item)(nil)).Column("id").Order("id").Scan(ctx, &ids))
	return ids
}

func TestSQLTransactor_CommitsWhenFnReturnsNil(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, save(ctx, db, "a"))
			return save(ctx, db, "b")
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, stored(t, ctx, db))
	})
}

func TestSQLTransactor_RollsBackWhenFnReturnsAnError(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		boom := errors.New("boom")

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, save(ctx, db, "a"))
			require.NoError(t, save(ctx, db, "b"))
			return boom
		})

		require.ErrorIs(t, err, boom, "WithTx returns the error fn returned")
		assert.Empty(t, stored(t, ctx, db), "neither write is kept")
	})
}

func TestSQLTransactor_RollsBackWhenFnPanics(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		assert.PanicsWithValue(t, "boom", func() {
			_ = dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
				require.NoError(t, save(ctx, db, "a"))
				panic("boom")
			})
		})

		assert.Empty(t, stored(t, ctx, db), "the write is rolled back, and the connection is free again")
	})
}

func TestSQLTransactor_RollbackKeepsWhatWasCommittedBefore(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		require.NoError(t, save(ctx, db, "before"))

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(ctx context.Context) error {
			require.NoError(t, save(ctx, db, "inside"))
			return errors.New("boom")
		})

		require.Error(t, err)
		assert.Equal(t, []string{"before"}, stored(t, ctx, db))
	})
}

func TestDB_IsTheTransactionInsideWithTxAndTheDatabaseOutside(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		assert.Same(t, db, dbtx.DB(ctx, db), "a context without a transaction selects the database")

		err := dbtx.NewSQLTransactor(db).WithTx(ctx, func(txCtx context.Context) error {
			_, ok := dbtx.DB(txCtx, db).(bun.Tx)
			assert.True(t, ok, "the context WithTx gives fn selects the transaction")
			_, ok = dbtx.DB(ctx, db).(bun.Tx)
			assert.False(t, ok, "the context WithTx was given still selects the database")
			return nil
		})
		require.NoError(t, err)

		assert.Same(t, db, dbtx.DB(ctx, db), "after WithTx returns, the database again")
	})
}

func TestSQLTransactor_ANestedWithTxJoinsTheOuterTransaction(t *testing.T) {
	forEachSQLBackend(t, func(t *testing.T, ctx context.Context, db *bun.DB) {
		transactor := dbtx.NewSQLTransactor(db)

		t.Run("commits with the outer", func(t *testing.T) {
			err := transactor.WithTx(ctx, func(ctx context.Context) error {
				require.NoError(t, save(ctx, db, "outer"))
				return transactor.WithTx(ctx, func(ctx context.Context) error {
					return save(ctx, db, "inner")
				})
			})

			require.NoError(t, err)
			assert.Equal(t, []string{"inner", "outer"}, stored(t, ctx, db))
		})

		t.Run("rolls back with the outer", func(t *testing.T) {
			boom := errors.New("boom")

			err := transactor.WithTx(ctx, func(ctx context.Context) error {
				require.NoError(t, transactor.WithTx(ctx, func(ctx context.Context) error {
					return save(ctx, db, "inner-2")
				}))
				return boom
			})

			require.ErrorIs(t, err, boom)
			assert.Equal(t, []string{"inner", "outer"}, stored(t, ctx, db), "the inner write that returned nil is rolled back too")
		})

		t.Run("an inner error reaches the outer", func(t *testing.T) {
			boom := errors.New("boom")

			err := transactor.WithTx(ctx, func(ctx context.Context) error {
				require.NoError(t, save(ctx, db, "outer-3"))
				return transactor.WithTx(ctx, func(context.Context) error { return boom })
			})

			require.ErrorIs(t, err, boom)
			assert.NotContains(t, stored(t, ctx, db), "outer-3")
		})
	})
}

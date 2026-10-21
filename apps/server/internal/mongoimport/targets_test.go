package mongoimport_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peekaping/internal/config"
	"peekaping/internal/infra"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

// sqliteTarget opens an empty SQLite database the way the app does, with foreign keys on.
func sqliteTarget(t *testing.T) *bun.DB {
	t.Helper()
	db, err := infra.ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "import.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

// postgresTarget opens an empty schema in the database at PEE_TEST_POSTGRES_DSN.
func postgresTarget(t *testing.T) *bun.DB {
	t.Helper()
	dsn := os.Getenv("PEE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set PEE_TEST_POSTGRES_DSN to run the import into PostgreSQL")
	}
	admin := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	schema := "pp227_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec("CREATE SCHEMA ?", bun.Ident(schema)); err != nil {
		_ = admin.Close()
		require.NoError(t, err)
	}
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

// forEachTarget runs fn with an empty database of each kind the import writes to.
func forEachTarget(t *testing.T, fn func(t *testing.T, db *bun.DB)) {
	t.Helper()
	t.Run("SQLite", func(t *testing.T) { fn(t, sqliteTarget(t)) })
	t.Run("Postgres", func(t *testing.T) { fn(t, postgresTarget(t)) })
}

// cut is what PostgreSQL keeps of a value that its column is too narrow for; SQLite keeps all of it.
func cut(db *bun.DB, value string, width int) string {
	if db.Dialect().Name() == dialect.PG && len(value) > width {
		return value[:width]
	}
	return value
}

package infra

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"
)

const autoVacuumIncremental = 2

func autoVacuumOf(t *testing.T, db *bun.DB) int {
	t.Helper()
	var mode int
	require.NoError(t, db.QueryRow("PRAGMA auto_vacuum").Scan(&mode))
	return mode
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// fillTable creates a table of rows of about 4 KB each
func fillTable(t *testing.T, db execer, rows int) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, "CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, payload BLOB)")
	require.NoError(t, err)
	for range rows {
		_, err = db.ExecContext(ctx, "INSERT INTO heartbeats (payload) VALUES (zeroblob(4000))")
		require.NoError(t, err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Size()
}

// A new database is created with auto_vacuum on, since SQLite ignores a change of it once the
// switch to WAL has written the first page
func TestProvideSQLDB_CreatesANewSQLiteDatabaseWithIncrementalAutoVacuum(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "quick"}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Equal(t, autoVacuumIncremental, autoVacuumOf(t, db))
	var journalMode string
	require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journalMode))
	assert.Equal(t, "wal", journalMode)
}

// The incremental vacuum of the shutdown hands the free pages back to the filesystem
func TestGracefulSQLiteShutdown_ShrinksTheFileAfterRowsAreDeleted(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "peekaping.db")
	logger := zap.NewNop().Sugar()

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "off"}, logger)
	require.NoError(t, err)
	fillTable(t, db, 500)
	_, err = db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	require.NoError(t, err)
	grown := fileSize(t, path)
	require.Greater(t, grown, int64(1_500_000), "the rows are in the main file")

	_, err = db.ExecContext(ctx, "DELETE FROM heartbeats WHERE id > 50")
	require.NoError(t, err)
	require.NoError(t, GracefulSQLiteShutdown(db, "sqlite", logger))

	assert.Less(t, fileSize(t, path), grown/4, "the freed pages left the file")
}

// A database created before this change has auto_vacuum off. Peekaping starts on it as before and
// doesn't rewrite it: the setting changes only with a VACUUM, which the docs describe.
func TestProvideSQLDB_LeavesAnExistingDatabaseWithoutAutoVacuumAsItIs(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	old, err := sql.Open(sqliteshim.ShimName, "file:"+path+"?mode=rwc")
	require.NoError(t, err)
	fillTable(t, old, 20)
	require.NoError(t, old.Close())
	sizeBefore := fileSize(t, path)

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "quick"}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	assert.Equal(t, 0, autoVacuumOf(t, db))
	var rows int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM heartbeats").Scan(&rows))
	assert.Equal(t, 20, rows)
	assert.Equal(t, sizeBefore, fileSize(t, path), "the file isn't rewritten")
}

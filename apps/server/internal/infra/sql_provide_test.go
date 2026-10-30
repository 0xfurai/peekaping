package infra

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"path/filepath"
	"testing"

	"peekaping/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// messages are what the observed logger logged, as "level: message" lines
func messages(logs *observer.ObservedLogs) []string {
	var lines []string
	for _, entry := range logs.All() {
		lines = append(lines, entry.Level.String()+": "+entry.Message)
	}
	return lines
}

func TestProvideSQLDB_SaysEachStepItCouldNotTakeOnADatabaseThatCantBeOpened(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	core, logs := observer.New(zapcore.DebugLevel)
	path := filepath.Join(t.TempDir(), "missing", "peekaping.db")

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.New(core).Sugar())

	assert.Nil(t, db)
	assert.EqualError(t, err, "failed to ping database: unable to open database file (14)")
	assert.Equal(t, []string{
		"warn: Failed to set auto_vacuum (non-fatal): unable to open database file (14)",
		"warn: Failed to check journal_mode (non-fatal): unable to open database file (14)",
		"warn: Failed to set journal_mode to WAL (non-fatal, current mode: ): unable to open database file (14)",
		"warn: Failed to run the SQLite quick check (non-fatal): unable to open database file (14)",
		"info: Connecting to SQLite database: " + path + " (WAL mode enabled, corruption prevention active)",
	}, messages(logs), "each step that fails is a warning, and the ping is what stops startup")
}

func TestProvideSQLDB_RefusesAnUnknownDatabaseType(t *testing.T) {
	db, err := ProvideSQLDB(&config.Config{DBType: "oracle"}, zap.NewNop().Sugar())

	assert.Nil(t, db)
	assert.EqualError(t, err, "unsupported database type: oracle. Supported types: postgres, mysql, sqlite")
}

// An empty DB_NAME is a database file in the working directory
func TestProvideSQLDB_OpensDataDBInTheWorkingDirectoryWithoutADatabaseName(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	dir := t.TempDir()
	t.Chdir(dir)
	core, logs := observer.New(zapcore.DebugLevel)

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite"}, zap.New(core).Sugar())

	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assert.FileExists(t, filepath.Join(dir, "data.db"))
	assert.Equal(t, 1, logs.FilterMessage("Connecting to SQLite database: ./data.db (WAL mode enabled, corruption prevention active)").Len())
}

// The server is not reached before the connection is tested, so a server that isn't there is
// the ping's error, after the line that says where it connected
func TestProvideSQLDB_PingsPostgreSQLAndMySQLAndSaysWhichItConnectedTo(t *testing.T) {
	for _, tc := range []struct{ dbType, line string }{
		{"postgres", "Connecting to PostgreSQL database: 127.0.0.1:1/peekaping"},
		{"postgresql", "Connecting to PostgreSQL database: 127.0.0.1:1/peekaping"},
		{"mysql", "Connecting to MySQL database: 127.0.0.1:1/peekaping"},
	} {
		t.Run(tc.dbType, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			cfg := &config.Config{DBType: tc.dbType, DBHost: "127.0.0.1", DBPort: "1", DBName: "peekaping", DBUser: "u", DBPass: "p"}

			db, err := ProvideSQLDB(cfg, zap.New(core).Sugar())

			assert.Nil(t, db)
			assert.ErrorContains(t, err, "failed to ping database: ")
			assert.Equal(t, []string{"info: " + tc.line}, messages(logs))
		})
	}
}

func TestProvideSQLDB_RefusesAMySQLAddressItCantParse(t *testing.T) {
	cfg := &config.Config{DBType: "mysql", DBHost: "127.0.0.1", DBPort: "1", DBName: "peekaping?x=%zz", DBUser: "u", DBPass: "p"}

	db, err := ProvideSQLDB(cfg, zap.NewNop().Sugar())

	assert.Nil(t, db)
	assert.ErrorContains(t, err, "failed to open MySQL connection: ")
}

// emptyCheck is a database whose every query returns no rows
type emptyCheck struct{}

func (emptyCheck) Connect(context.Context) (driver.Conn, error) { return emptyConn{}, nil }
func (emptyCheck) Driver() driver.Driver                        { return nil }

type emptyConn struct{}

func (emptyConn) Prepare(string) (driver.Stmt, error) { return nil, io.ErrUnexpectedEOF }
func (emptyConn) Close() error                        { return nil }
func (emptyConn) Begin() (driver.Tx, error)           { return nil, io.ErrUnexpectedEOF }
func (emptyConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return emptyRows{}, nil
}

type emptyRows struct{}

func (emptyRows) Columns() []string         { return []string{"quick_check"} }
func (emptyRows) Close() error              { return nil }
func (emptyRows) Next([]driver.Value) error { return io.EOF }

// A check that returns no rows says nothing is wrong and nothing is right: it is a warning too
func TestCheckSQLiteOnStartup_WarnsAndGoesOnWhenTheCheckReturnsNoResult(t *testing.T) {
	db := bun.NewDB(sql.OpenDB(emptyCheck{}), sqlitedialect.New())
	core, logs := observer.New(zapcore.DebugLevel)

	require.NoError(t, checkSQLiteOnStartup(db, "peekaping.db", "full", zap.New(core).Sugar()))

	assert.Equal(t, []string{"warn: Failed to run the SQLite full check (non-fatal): it returned no result"}, messages(logs))
}

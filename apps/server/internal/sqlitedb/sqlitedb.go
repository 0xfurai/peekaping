// Package sqlitedb opens the SQLite database for the server and for migrate. It imports only the
// driver, so migrate doesn't link the server's Redis, Mongo and queue dependencies.
package sqlitedb

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/uptrace/bun/driver/sqliteshim"
)

// connectionPragmas are the settings of one SQLite connection. SQLite resets them when a
// connection closes, and database/sql opens a new connection whenever it drops one (a query whose
// context is cancelled costs its connection). So they ride in the DSN, where the driver applies
// them to every connection it opens; a PRAGMA run once at startup would reach only the first.
// busy_timeout is applied before the others, so they can wait for a lock.
var connectionPragmas = []string{
	"busy_timeout(5000)",
	"synchronous(NORMAL)",      // a good balance between speed and safety in multi-process use
	"foreign_keys(ON)",         // data integrity: the ON DELETE CASCADE rules
	"temp_store(MEMORY)",       // less disk I/O and corruption risk for temporary tables
	"wal_autocheckpoint(1000)", // pages; keeps the write-ahead log from growing too large
	"cache_size(-64000)",       // negative is KiB: 64 MB
}

// DSN returns the DSN to open the SQLite database at path with: the settings of every
// connection, and transactions that begin with BEGIN IMMEDIATE. A deferred transaction that reads
// and then writes fails at once with "database is locked (517)" (SQLITE_BUSY_SNAPSHOT), without
// waiting for busy_timeout, when another process committed in between. An immediate one takes the
// write lock first and waits for it. The driver is modernc.org/sqlite, which sqliteshim picks on
// every platform the images build for.
//
// cache=shared shares one cache between the connections of a process; mode=rwc creates the file.
func DSN(path string) string {
	query := url.Values{
		"cache":   {"shared"},
		"mode":    {"rwc"},
		"_txlock": {"immediate"},
		"_pragma": connectionPragmas,
	}
	return fmt.Sprintf("file:%s?%s", path, query.Encode())
}

// Open opens the SQLite database at path for the server and for migrate, so both run with
// the same settings. It limits the pool to one connection, which serializes access and prevents
// lock contention inside a process. Connections never expire, which matters in WAL mode.
func Open(path string) (*sql.DB, error) {
	sqldb, err := sql.Open(sqliteshim.ShimName, DSN(path))
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite connection: %w", err)
	}
	sqldb.SetMaxOpenConns(1)
	sqldb.SetMaxIdleConns(1)
	sqldb.SetConnMaxLifetime(0)
	return sqldb, nil
}

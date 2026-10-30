package main

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/config"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/sqliteshim"
)

// migrate opens SQLite with the settings the server uses, on every connection: database/sql
// drops a connection when its query's context is cancelled, and the next one must have them too.
func TestConnectToDatabase_SQLiteSettingsSurviveACancelledQuery(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	db, err := connectToDatabase(&config.DBConfig{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "peekaping.db")})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertSQLiteSettings(t, db, "at startup")

	// A TEMP table lives as long as its connection, so it is the marker: when it is gone, the
	// settings checked next are a new connection's, and the test can't pass when the cancel never
	// reached the driver.
	replaced := false
	for attempt := 0; attempt < 20 && !replaced; attempt++ {
		if _, err := db.Exec("CREATE TEMP TABLE IF NOT EXISTS connection_marker (x INTEGER)"); err != nil {
			t.Fatalf("create the marker: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(50*time.Millisecond, cancel)
		var n int64
		err := db.NewRaw("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 2000000000) SELECT count(*) FROM c").Scan(ctx, &n)
		timer.Stop()
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("the slow query failed for another reason: %v", err)
		}
		var markers int
		if err := db.NewRaw("SELECT count(*) FROM sqlite_temp_master WHERE name = 'connection_marker'").Scan(context.Background(), &markers); err != nil {
			t.Fatalf("look for the marker: %v", err)
		}
		replaced = markers == 0
	}
	if !replaced {
		t.Fatal("the cancelled query never cost the connection")
	}

	assertSQLiteSettings(t, db, "after a cancelled query")
}

func autoVacuumOf(t *testing.T, db *bun.DB) int {
	t.Helper()
	var autoVacuum int
	if err := db.NewRaw("PRAGMA auto_vacuum").Scan(context.Background(), &autoVacuum); err != nil {
		t.Fatalf("read auto_vacuum: %v", err)
	}
	return autoVacuum
}

// A database that bun creates (migrate on a first start) is made with auto_vacuum on: once the
// switch to WAL has written the first page, SQLite ignores a change of it.
func TestConnectToDatabase_CreatesANewSQLiteDatabaseWithIncrementalAutoVacuum(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	db, err := connectToDatabase(&config.DBConfig{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "peekaping.db")})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := autoVacuumOf(t, db); got != 2 {
		t.Errorf("auto_vacuum of a new database: got %d, want 2 (incremental)", got)
	}
}

// A database made before this change has auto_vacuum off, and bun leaves it that way
func TestConnectToDatabase_LeavesAnExistingSQLiteDatabaseWithoutAutoVacuumAsItIs(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	old, err := sql.Open(sqliteshim.ShimName, "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatalf("open the old database: %v", err)
	}
	if _, err := old.Exec("CREATE TABLE heartbeats (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("create the old database: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close the old database: %v", err)
	}

	db, err := connectToDatabase(&config.DBConfig{DBType: "sqlite", DBName: path})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := autoVacuumOf(t, db); got != 0 {
		t.Errorf("auto_vacuum of an old database: got %d, want 0", got)
	}
}

func assertSQLiteSettings(t *testing.T, db *bun.DB, when string) {
	t.Helper()
	for pragma, want := range map[string]int{
		"foreign_keys":       1,
		"busy_timeout":       5000,
		"synchronous":        1,
		"temp_store":         2,
		"wal_autocheckpoint": 1000,
		"cache_size":         -64000,
	} {
		var got int
		if err := db.NewRaw("PRAGMA "+pragma).Scan(context.Background(), &got); err != nil {
			t.Fatalf("read %s %s: %v", pragma, when, err)
		}
		if got != want {
			t.Errorf("%s %s: got %d, want %d", pragma, when, got, want)
		}
	}
}

package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"peekaping/internal/config"

	"github.com/uptrace/bun"
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

	cancelled := false
	for attempt := 0; attempt < 20 && !cancelled; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(50*time.Millisecond, cancel)
		var n int64
		err := db.NewRaw("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 2000000000) SELECT count(*) FROM c").Scan(ctx, &n)
		timer.Stop()
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("the slow query failed for another reason: %v", err)
			}
			cancelled = true
		}
	}
	if !cancelled {
		t.Fatal("the slow query was never cancelled")
	}

	assertSQLiteSettings(t, db, "after a cancelled query")
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

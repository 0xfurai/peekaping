package infra

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"

	"peekaping/internal/config"
)

// A web client that hangs up mid-request cancels the API's read of heartbeats.
// The SQLite driver must close that read's statement when it is cancelled. If
// it doesn't, the API's only connection keeps a stale read snapshot open, and
// its next write fails with "database is locked (517)" once the ingester, a
// separate process, has written to the database in between.
//
// Each round reads with a random cancel time, then lets the ingester write a
// heartbeat, then expects the API to write one too.
func TestProvideSQLDB_WriteAfterCancelledRead(t *testing.T) {
	// The query hook prints every failed query, and a cancelled read is one;
	// switch it off so the test doesn't depend on the caller's BUNDEBUG.
	t.Setenv("BUNDEBUG", "0")

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "peekaping.db")

	api, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = api.Close() })

	// The ingester: its own connection to the same file, no shared cache, and
	// a busy timeout so it waits out a lock instead of failing.
	ingester, err := sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?mode=rwc", path))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ingester.Close() })
	ingester.SetMaxOpenConns(1) // so the PRAGMA below applies to every statement
	_, err = ingester.ExecContext(ctx, "PRAGMA busy_timeout=5000")
	require.NoError(t, err)

	_, err = api.ExecContext(ctx, "CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, response_time INTEGER)")
	require.NoError(t, err)
	for i := range 200 {
		_, err = api.ExecContext(ctx, "INSERT INTO heartbeats (response_time) VALUES (?)", i)
		require.NoError(t, err)
	}

	for i := range 5000 {
		// Cancel somewhere between before the first row and after the last one.
		// Whether the read finishes or not doesn't matter here, only that the
		// API connection is usable afterwards.
		readCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(time.Duration(rand.IntN(300))*time.Microsecond, cancel)
		var ids []int64
		_ = api.NewSelect().Table("heartbeats").Column("id").Scan(readCtx, &ids)
		timer.Stop()
		cancel()

		_, err = ingester.ExecContext(ctx, "UPDATE heartbeats SET response_time = response_time + 1 WHERE id = 1")
		require.NoError(t, err, "iteration %d: the ingester's write failed", i)

		_, err = api.ExecContext(ctx, "UPDATE heartbeats SET response_time = response_time + 1 WHERE id = 2")
		require.NoError(t, err, "iteration %d: the API's write after a cancelled read failed", i)
	}
}

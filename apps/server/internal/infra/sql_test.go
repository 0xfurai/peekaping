package infra

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/sqliteshim"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

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

	for i := range 20000 {
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

// recordQueries is a bun query hook that keeps every query it sees
type recordQueries struct{ queries []string }

func (r *recordQueries) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

func (r *recordQueries) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	r.queries = append(r.queries, event.Query)
}

// A full integrity check reads the whole file: about 30s for 1.5 GB, which 24h of 1,000
// monitors at 20s fill. At shutdown it outlasted peekaping's 30s deadline and docker stop's
// grace period, so the shutdown checkpoints and closes without one.
func TestGracefulSQLiteShutdown_CheckpointsAndClosesWithoutAFullIntegrityCheck(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, msg TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO heartbeats (msg) VALUES ('200 - OK')")
	require.NoError(t, err)
	hook := &recordQueries{}
	db.AddQueryHook(hook)

	require.NoError(t, GracefulSQLiteShutdown(db, "sqlite", zap.NewNop().Sugar()))

	require.Contains(t, hook.queries, "PRAGMA wal_checkpoint(TRUNCATE)")
	for _, query := range hook.queries {
		require.NotContains(t, query, "integrity_check")
	}
	require.Error(t, db.Ping(), "the database is still open")
	wal, err := os.Stat(path + "-wal")
	if err == nil {
		require.Zero(t, wal.Size(), "the write-ahead log wasn't checkpointed")
	} else {
		require.ErrorIs(t, err, os.ErrNotExist)
	}
}

// writeCorruptSQLiteDatabase builds a heartbeat-sized database at path with ProvideSQLDB and then
// damages it: the start of one table page is overwritten with 0xFF, which `PRAGMA quick_check`
// reports as findings. Page 1 is the schema and page 4 is a data page of the heartbeats table,
// whose b-tree SQLite can no longer read.
func writeCorruptSQLiteDatabase(t *testing.T, path string) {
	t.Helper()
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, msg TEXT)")
	require.NoError(t, err)
	_, err = db.Exec("WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 3000) " +
		"INSERT INTO heartbeats (msg) SELECT printf('%0200d', x) FROM n")
	require.NoError(t, err)
	var pageSize, pageCount int
	require.NoError(t, db.QueryRow("PRAGMA page_size").Scan(&pageSize))
	require.NoError(t, db.QueryRow("PRAGMA page_count").Scan(&pageCount))
	require.Greater(t, pageCount, 4, "the table should span several pages")
	// the closed database leaves no write-ahead log behind, so the damage is in the main file
	_, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	require.NoError(t, err)
	damage := make([]byte, 200)
	for i := range damage {
		damage[i] = 0xFF
	}
	_, err = file.WriteAt(damage, int64(3*pageSize))
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func sha256OfFile(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return sha256.Sum256(content)
}

// A database that fails the startup check is damaged, and a process that goes on writing to it
// makes the damage worse. Startup stops with an error instead, and the file is left as it was,
// so the operator can restore a backup or salvage the data.
func TestProvideSQLDB_StopsOnACorruptDatabase(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	writeCorruptSQLiteDatabase(t, path)
	before := sha256OfFile(t, path)

	core, _ := observer.New(zapcore.DebugLevel)
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.New(core).Sugar())
	if db != nil {
		t.Cleanup(func() { _ = db.Close() })
	}

	require.Error(t, err)
	require.Nil(t, db)
	require.ErrorContains(t, err, "is corrupt")
	require.ErrorContains(t, err, "https://docs.peekaping.com/self-hosting/sqlite-recovery")
	require.Equal(t, before, sha256OfFile(t, path), "the database file was changed")
}

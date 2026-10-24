package infra

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
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
// so the operator can restore a backup or salvage the data. Before it stops, the log tells the
// operator what SQLite found and what to do. Both checks stop it: the quick check by default, and
// the full check when SQLITE_STARTUP_CHECK says so.
func TestProvideSQLDB_StopsOnACorruptDatabase(t *testing.T) {
	for _, check := range []struct{ name, mode, label, pragma string }{
		{"unset means quick", "", "quick", "quick_check"},
		{"quick", "quick", "quick", "quick_check"},
		{"full", "full", "full", "integrity_check"},
	} {
		t.Run(check.name, func(t *testing.T) {
			t.Setenv("BUNDEBUG", "0")
			path := filepath.Join(t.TempDir(), "peekaping.db")
			writeCorruptSQLiteDatabase(t, path)
			before := sha256OfFile(t, path)

			core, logs := observer.New(zapcore.DebugLevel)
			db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: check.mode}, zap.New(core).Sugar())
			if db != nil {
				t.Cleanup(func() { _ = db.Close() })
			}

			require.Error(t, err)
			require.Nil(t, db)
			require.ErrorContains(t, err, "is corrupt")
			// the first finding, on one line: SQLite's first row is two lines, "*** in database main ***"
			// and the finding
			require.ErrorContains(t, err, "Tree 2 page 4: btreeInitPage() returns error code 11")
			require.NotContains(t, err.Error(), "\n")
			require.ErrorContains(t, err, "https://docs.peekaping.com/self-hosting/sqlite-recovery")
			require.Equal(t, before, sha256OfFile(t, path), "the database file was changed")

			// the report: what stopped, every finding, then the procedure with the docs link
			errorEntries := logs.FilterLevelExact(zapcore.ErrorLevel).All()
			require.GreaterOrEqual(t, len(errorEntries), 3, "the stop, at least one finding, the procedure")
			require.Equal(t,
				"SQLite "+check.label+" check found corruption in "+path+". Peekaping stops here and doesn't change the file.",
				errorEntries[0].Message)
			findings := errorEntries[1 : len(errorEntries)-1]
			for _, finding := range findings {
				require.True(t, strings.HasPrefix(finding.Message, check.pragma+": "), finding.Message)
			}
			require.Contains(t, findings[len(findings)-1].Message, "btreeInitPage() returns error code 11")
			procedure := errorEntries[len(errorEntries)-1].Message
			for _, step := range []string{
				"Stop every Peekaping process that uses this database",
				"Keep the original database file together with its -wal and -shm files",
				"Restore a tested backup",
				"sqlite3 .recover",
				"PRAGMA integrity_check",
				"https://docs.peekaping.com/self-hosting/sqlite-recovery",
			} {
				require.Contains(t, procedure, step)
			}
			// the old startup logged "Database integrity check FAILED" and ran the full check
			for _, entry := range logs.All() {
				require.NotContains(t, entry.Message, "Database integrity check")
			}
		})
	}
}

// SQLITE_STARTUP_CHECK=off is the operator's choice not to check at startup, say because a
// database of many gigabytes makes even the quick check slow, or because the file is known to
// have findings that are being dealt with. The database opens as it did before a finding could
// stop startup, and the log says that no check ran.
func TestProvideSQLDB_OpensACorruptDatabaseWhenTheStartupCheckIsOff(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	writeCorruptSQLiteDatabase(t, path)

	core, logs := observer.New(zapcore.DebugLevel)
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "off"}, zap.New(core).Sugar())
	if db != nil {
		t.Cleanup(func() { _ = db.Close() })
	}

	require.NoError(t, err)
	require.NotNil(t, db)
	require.Empty(t, logs.FilterLevelExact(zapcore.ErrorLevel).All())
	require.Len(t, logs.FilterMessage("SQLite startup check skipped: SQLITE_STARTUP_CHECK is off").All(), 1)
}

// A crash leaves a hot write-ahead log: committed changes that aren't in the main file yet. Closing
// SQLite's last connection checkpoints that log into the main file and deletes the -wal and -shm
// files, which the operator is told to keep with the original database. So a startup that stops on
// corruption must leave the database open, and the -wal file and the main file as they were.
//
// The crash is a copy of a live database, taken while a writer holds it open: the copy has a -wal
// file with frames and no process using it.
func TestProvideSQLDB_StopsOnACorruptDatabase_LeavesTheWriteAheadLogAlone(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	ctx := context.Background()
	root := t.TempDir()
	livePath := filepath.Join(root, "live", "peekaping.db")
	crashedPath := filepath.Join(root, "crashed", "peekaping.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(livePath), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(crashedPath), 0o755))

	// The writer: its own connection, no shared cache. Closing it would checkpoint the log, so
	// it stays open until the test ends.
	writer, err := sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?mode=rwc", livePath))
	require.NoError(t, err)
	t.Cleanup(func() { _ = writer.Close() })
	writer.SetMaxOpenConns(1) // so the PRAGMAs below apply to every statement
	_, err = writer.ExecContext(ctx, "PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, "CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, msg TEXT)")
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 3000) "+
		"INSERT INTO heartbeats (msg) SELECT printf('%0200d', x) FROM n")
	require.NoError(t, err)
	var pageSize, pageCount int
	require.NoError(t, writer.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize))
	require.NoError(t, writer.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount))
	require.Greater(t, pageCount, 4, "the table should span several pages")
	// the table is in the main file; what is written next is only in the log
	_, err = writer.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	require.NoError(t, err)
	_, err = writer.ExecContext(ctx, "WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < 10) "+
		"INSERT INTO heartbeats (msg) SELECT printf('%0200d', x) FROM n")
	require.NoError(t, err)

	// the crash: the files as they are while the writer holds the database
	for _, suffix := range []string{"", "-wal", "-shm"} {
		content, err := os.ReadFile(livePath + suffix)
		if suffix == "-shm" && os.IsNotExist(err) {
			continue
		}
		require.NoError(t, err)
		if suffix == "-wal" {
			require.NotEmpty(t, content, "the crashed database should have a hot write-ahead log")
		}
		require.NoError(t, os.WriteFile(crashedPath+suffix, content, 0o644))
	}

	// The damage is in a table page of the main file. The rows added after the checkpoint are on
	// the table's last page, so the log doesn't replace the damaged page.
	file, err := os.OpenFile(crashedPath, os.O_WRONLY, 0)
	require.NoError(t, err)
	damage := make([]byte, 200)
	for i := range damage {
		damage[i] = 0xFF
	}
	_, err = file.WriteAt(damage, int64(3*pageSize))
	require.NoError(t, err)
	require.NoError(t, file.Close())
	databaseBefore := sha256OfFile(t, crashedPath)
	logBefore := sha256OfFile(t, crashedPath+"-wal")

	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: crashedPath}, zap.NewNop().Sugar())
	if db != nil {
		t.Cleanup(func() { _ = db.Close() })
	}

	require.Error(t, err)
	require.Nil(t, db)
	require.ErrorContains(t, err, "is corrupt")
	require.FileExists(t, crashedPath+"-wal", "the write-ahead log was deleted")
	require.Equal(t, logBefore, sha256OfFile(t, crashedPath+"-wal"), "the write-ahead log was changed")
	require.Equal(t, databaseBefore, sha256OfFile(t, crashedPath), "the database file was changed")
}

// A full integrity check reads the whole file at every start: about 30s for 1.5 GB, against 1.6s
// for the quick check. So a healthy database starts with the quick check only, unless
// SQLITE_STARTUP_CHECK asks for the full one, or for none. The log says which check passed, or
// that none ran.
func TestCheckSQLiteOnStartup_RunsOnlyTheCheckTheModeNames(t *testing.T) {
	for _, check := range []struct {
		name, mode string
		queries    []string
		message    string
	}{
		{"unset means quick", "", []string{"PRAGMA quick_check"}, "SQLite quick check passed"},
		{"quick", "quick", []string{"PRAGMA quick_check"}, "SQLite quick check passed"},
		{"full", "full", []string{"PRAGMA integrity_check"}, "SQLite full check passed"},
		{"off", "off", nil, "SQLite startup check skipped: SQLITE_STARTUP_CHECK is off"},
	} {
		t.Run(check.name, func(t *testing.T) {
			t.Setenv("BUNDEBUG", "0")
			path := filepath.Join(t.TempDir(), "peekaping.db")
			db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "off"}, zap.NewNop().Sugar())
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			_, err = db.Exec("CREATE TABLE heartbeats (id INTEGER PRIMARY KEY, msg TEXT)")
			require.NoError(t, err)
			_, err = db.Exec("INSERT INTO heartbeats (msg) VALUES ('200 - OK')")
			require.NoError(t, err)
			hook := &recordQueries{}
			db.AddQueryHook(hook)
			core, logs := observer.New(zapcore.DebugLevel)

			require.NoError(t, checkSQLiteOnStartup(db, path, check.mode, zap.New(core).Sugar()))

			require.Equal(t, check.queries, hook.queries, "the queries the startup check ran")
			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, zapcore.InfoLevel, entries[0].Level)
			require.Equal(t, check.message, entries[0].Message)
		})
	}
}

// A mode that no config validation let through would otherwise skip the check without a word, so
// startup stops instead.
func TestCheckSQLiteOnStartup_RefusesAnUnknownMode(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path, SQLiteStartupCheck: "off"}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	hook := &recordQueries{}
	db.AddQueryHook(hook)

	err = checkSQLiteOnStartup(db, path, "fast", zap.NewNop().Sugar())

	require.EqualError(t, err, `unsupported SQLITE_STARTUP_CHECK "fast". Supported values: quick, full, off`)
	require.Empty(t, hook.queries)
}

// A check that can't run, say because another process holds a lock, isn't a finding: it's a
// warning, and startup goes on as it did before the check could stop it.
func TestCheckSQLiteOnStartup_WarnsAndGoesOnWhenTheCheckCantRun(t *testing.T) {
	t.Setenv("BUNDEBUG", "0")
	path := filepath.Join(t.TempDir(), "peekaping.db")
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	require.NoError(t, db.Close()) // every query on it fails now
	core, logs := observer.New(zapcore.DebugLevel)

	require.NoError(t, checkSQLiteOnStartup(db, path, "quick", zap.New(core).Sugar()))

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, zapcore.WarnLevel, entries[0].Level)
	require.Contains(t, entries[0].Message, "Failed to run the SQLite quick check (non-fatal): ")
}

// sqliteSettings is what a SQLite connection of the app must have, read back with the PRAGMA
// that reports each one
var sqliteSettings = []struct {
	pragma string
	want   int
}{
	{"foreign_keys", 1},
	{"busy_timeout", 5000},
	{"synchronous", 1},
	{"temp_store", 2},
	{"wal_autocheckpoint", 1000},
	{"cache_size", -64000},
}

func assertSQLiteSettings(t *testing.T, ctx context.Context, db bun.IDB, when string) {
	t.Helper()
	for _, s := range sqliteSettings {
		var got int
		require.NoError(t, db.NewRaw("PRAGMA "+s.pragma).Scan(ctx, &got), s.pragma)
		require.Equal(t, s.want, got, "%s %s", s.pragma, when)
	}
}

// cancelASlowQuery runs a query that takes seconds and cancels its context mid-flight, until the
// connection it ran on has been replaced. database/sql drops that connection once the driver
// reports the cancellation, and the next query gets a new one. A TEMP table lives as long as its
// connection, so it is the marker: when it is gone, the settings checked next are a new
// connection's, and the test can't pass when the cancel never reached the driver.
func cancelASlowQuery(t *testing.T, db *bun.DB) {
	t.Helper()
	ctx := context.Background()
	for attempt := 0; attempt < 20; attempt++ {
		_, err := db.ExecContext(ctx, "CREATE TEMP TABLE IF NOT EXISTS connection_marker (x INTEGER)")
		require.NoError(t, err)

		slowCtx, cancel := context.WithCancel(ctx)
		timer := time.AfterFunc(50*time.Millisecond, cancel)
		var n int64
		err = db.NewRaw("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 2000000000) SELECT count(*) FROM c").Scan(slowCtx, &n)
		timer.Stop()
		cancel()
		if err != nil {
			require.ErrorIs(t, err, context.Canceled)
		}

		var markers int
		require.NoError(t, db.NewRaw("SELECT count(*) FROM sqlite_temp_master WHERE name = 'connection_marker'").Scan(ctx, &markers))
		if markers == 0 {
			return
		}
	}
	t.Fatal("the cancelled query never cost the connection")
}

// A query whose context is cancelled costs its connection: database/sql closes it and opens a new
// one, which must have the same settings.
func TestProvideSQLDB_ASettingSurvivesACancelledQuery(t *testing.T) {
	// The query hook prints every failed query, and a cancelled one is one
	t.Setenv("BUNDEBUG", "0")
	db, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: filepath.Join(t.TempDir(), "peekaping.db")}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	assertSQLiteSettings(t, context.Background(), db, "at startup")

	cancelASlowQuery(t, db)

	assertSQLiteSettings(t, context.Background(), db, "after a cancelled query")
}

// A transaction that reads and then writes must wait for a writer that got in between, like any
// write does, and not fail with "database is locked (517)". The monitor status update and the
// monitor delete are such transactions. A is the server, B another process that writes to the same
// file (the ingester). B has no shared cache, since processes don't share one; two connections of
// one process would lock each other by table instead, and hide the failure.
func TestProvideSQLDB_ATransactionThatReadsThenWritesWaitsForAnotherWriter(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "peekaping.db")
	a, err := ProvideSQLDB(&config.Config{DBType: "sqlite", DBName: path}, zap.NewNop().Sugar())
	require.NoError(t, err)
	t.Cleanup(func() { _ = a.Close() })
	b, err := sql.Open(sqliteshim.ShimName, fmt.Sprintf("file:%s?mode=rwc&_pragma=busy_timeout(5000)", path))
	require.NoError(t, err)
	b.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = b.Close() })
	_, err = a.ExecContext(ctx, "CREATE TABLE writes (id INTEGER PRIMARY KEY, who TEXT)")
	require.NoError(t, err)

	tx, err := a.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	var rows int
	require.NoError(t, tx.NewRaw("SELECT count(*) FROM writes").Scan(ctx, &rows))

	bDone := make(chan error, 1)
	go func() {
		_, err := b.ExecContext(ctx, "INSERT INTO writes (who) VALUES ('B')")
		bDone <- err
	}()
	// Let B get its write in if the lock doesn't keep it out
	select {
	case err := <-bDone:
		bDone <- err
	case <-time.After(300 * time.Millisecond):
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO writes (who) VALUES ('A')")
	require.NoError(t, err, "A's write after its read")
	require.NoError(t, tx.Commit())

	select {
	case err := <-bDone:
		require.NoError(t, err, "B's write")
	case <-time.After(10 * time.Second):
		t.Fatal("B's write never finished")
	}
	var who []string
	require.NoError(t, a.NewRaw("SELECT who FROM writes ORDER BY id").Scan(ctx, &who))
	require.Equal(t, []string{"A", "B"}, who, "B waits for A's transaction")
}
